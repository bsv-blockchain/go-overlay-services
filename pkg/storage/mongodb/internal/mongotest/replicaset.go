package mongotest

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

const (
	memberTotal       = 3
	startupTimeout    = 45 * time.Second
	shutdownTimeout   = 15 * time.Second
	gracefulStopWait  = 2 * time.Second
	connectionTimeout = 3 * time.Second
	pollInterval      = 100 * time.Millisecond
)

var (
	errMemberIndex        = errors.New("MongoDB member index is out of range")
	errMongodExited       = errors.New("mongod exited unexpectedly")
	errReplicaUnavailable = errors.New("MongoDB replica-set client is unavailable")
)

// ReplicaSet is an isolated, local three-member MongoDB replica set. Client is
// connected through the replica-set URI with majority writes enabled.
type ReplicaSet struct {
	URI    string
	Client *mongo.Client

	mu      sync.Mutex
	mongod  string
	name    string
	members []*member
}

type member struct {
	address string
	dbPath  string
	logPath string
	cmd     *exec.Cmd
	wait    chan error
}

// New creates and initializes a disposable local replica set. The test is
// skipped unless GO_OVERLAY_MONGO_TESTS is exactly "1". MONGOD_BIN can select
// a binary; otherwise mongod is resolved through PATH.
func New(t testing.TB) *ReplicaSet {
	t.Helper()
	if os.Getenv("GO_OVERLAY_MONGO_TESTS") != "1" {
		t.Skip("set GO_OVERLAY_MONGO_TESTS=1 to run disposable MongoDB integration tests")
	}

	mongod, err := resolveMongod()
	if err != nil {
		t.Fatalf("resolve mongod: %v", err)
	}
	addresses, err := reserveLoopbackAddresses(memberTotal)
	if err != nil {
		t.Fatalf("reserve MongoDB member addresses: %v", err)
	}

	root := t.TempDir()
	_, firstPort, splitErr := net.SplitHostPort(addresses[0])
	if splitErr != nil {
		t.Fatalf("split reserved MongoDB member address: %v", splitErr)
	}
	replica := &ReplicaSet{
		mongod:  mongod,
		name:    "go_overlay_test_" + firstPort,
		members: make([]*member, memberTotal),
	}
	for index, address := range addresses {
		memberRoot := filepath.Join(root, fmt.Sprintf("member-%d", index))
		dbPath := filepath.Join(memberRoot, "data")
		if err = os.MkdirAll(dbPath, 0o700); err != nil {
			t.Fatalf("create MongoDB member %d data directory: %v", index, err)
		}
		replica.members[index] = &member{
			address: address,
			dbPath:  dbPath,
			logPath: filepath.Join(memberRoot, "mongod.log"),
		}
	}
	t.Cleanup(func() { replica.cleanup(t) })

	ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	defer cancel()
	if err = replica.start(ctx); err != nil {
		t.Fatalf("start disposable MongoDB replica set: %v", err)
	}
	return replica
}

// MemberCount returns the fixed number of replica-set members.
func (r *ReplicaSet) MemberCount() int { return len(r.members) }

// MemberAddress returns a member's loopback host:port address.
func (r *ReplicaSet) MemberAddress(index int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if index < 0 || index >= len(r.members) {
		return ""
	}
	return r.members[index].address
}

// FailCommand enables the failCommand failpoint on the replica-set primary.
// A non-positive times value leaves the failpoint enabled until DisableFailPoint.
func (r *ReplicaSet) FailCommand(ctx context.Context, times int32, commands []string, errorCode int32, labels []string) error {
	client := r.replicaClient()
	if client == nil {
		return errReplicaUnavailable
	}
	failCommands := make(bson.A, len(commands))
	for index, command := range commands {
		failCommands[index] = command
	}
	errorLabels := make(bson.A, len(labels))
	for index, label := range labels {
		errorLabels[index] = label
	}
	var mode any = "alwaysOn"
	if times > 0 {
		mode = bson.D{{Key: "times", Value: times}}
	}
	return client.Database("admin").RunCommand(ctx, bson.D{
		{Key: "configureFailPoint", Value: "failCommand"},
		{Key: "mode", Value: mode},
		{Key: "data", Value: bson.D{
			{Key: "failCommands", Value: failCommands},
			{Key: "errorCode", Value: errorCode},
			{Key: "errorLabels", Value: errorLabels},
		}},
	}).Err()
}

// DisableFailPoint turns off failCommand on the replica-set primary.
func (r *ReplicaSet) DisableFailPoint(ctx context.Context) error {
	client := r.replicaClient()
	if client == nil {
		return nil
	}
	return client.Database("admin").RunCommand(ctx, bson.D{
		{Key: "configureFailPoint", Value: "failCommand"},
		{Key: "mode", Value: "off"},
	}).Err()
}

func (r *ReplicaSet) replicaClient() *mongo.Client {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Client
}

// MemberClient opens a direct client to one member. The caller owns the client
// and must disconnect it when finished.
func (r *ReplicaSet) MemberClient(ctx context.Context, index int) (*mongo.Client, error) {
	address := r.MemberAddress(index)
	if address == "" {
		return nil, fmt.Errorf("%w: %d", errMemberIndex, index)
	}
	return connectDirect(ctx, address)
}

// StopMember stops one owned child process and waits for a primary with a
// majority of the remaining tracked members. It is safe to call repeatedly.
func (r *ReplicaSet) StopMember(ctx context.Context, index int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.memberAt(index); err != nil {
		return err
	}
	if err := r.stopMemberLocked(ctx, r.members[index]); err != nil {
		return err
	}
	return r.waitForReplicaLocked(ctx, memberTotal/2+1)
}

// StartMember starts a previously stopped owned member and waits until all
// three members are available with a primary. It is safe to call repeatedly.
func (r *ReplicaSet) StartMember(ctx context.Context, index int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.memberAt(index); err != nil {
		return err
	}
	member := r.members[index]
	if member.cmd != nil {
		return r.waitForReplicaLocked(ctx, memberTotal)
	}
	if err := r.startMemberLocked(ctx, member); err != nil {
		return err
	}
	return r.waitForReplicaLocked(ctx, memberTotal)
}

func (r *ReplicaSet) start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, member := range r.members {
		if err := r.startMemberLocked(ctx, member); err != nil {
			return err
		}
	}
	if err := r.initiateLocked(ctx); err != nil {
		return err
	}
	if err := r.waitForReplicaLocked(ctx, memberTotal); err != nil {
		return err
	}
	client, err := mongo.Connect(options.Client().ApplyURI(r.URI).SetServerSelectionTimeout(10 * time.Second).SetConnectTimeout(connectionTimeout))
	if err != nil {
		return fmt.Errorf("connect replica-set client: %w", err)
	}
	if err = waitFor(ctx, func() (bool, error) {
		pingCtx, cancel := context.WithTimeout(ctx, connectionTimeout)
		defer cancel()
		return client.Ping(pingCtx, readpref.Primary()) == nil, nil
	}); err != nil {
		disconnectClient(ctx, client)
		return fmt.Errorf("ping replica-set primary: %w", err)
	}
	r.Client = client
	return nil
}

func (r *ReplicaSet) startMemberLocked(ctx context.Context, member *member) error {
	if member.cmd != nil {
		return nil
	}
	//nolint:gosec // mongod is resolved by LookPath and all arguments are harness-owned values.
	// CommandContext still gives this owned child an explicit lifecycle; cleanup
	// signals the child instead of tying it to the caller's startup deadline.
	command := exec.CommandContext(context.WithoutCancel(ctx), r.mongod, r.memberArgs(member)...)
	if err := command.Start(); err != nil {
		return fmt.Errorf("start mongod at %s: %w", member.address, err)
	}
	member.cmd = command
	member.wait = make(chan error, 1)
	go func() { member.wait <- command.Wait() }()
	if err := r.waitForMemberLocked(ctx, member); err != nil {
		return err
	}
	return nil
}

func (r *ReplicaSet) memberArgs(member *member) []string {
	_, port, _ := net.SplitHostPort(member.address)
	return []string{
		"--replSet", r.name,
		"--bind_ip", "127.0.0.1",
		"--port", port,
		"--dbpath", member.dbPath,
		"--logpath", member.logPath,
		"--logappend",
		"--nounixsocket",
		"--oplogSize", "50",
		"--wiredTigerCacheSizeGB", "0.25",
		"--setParameter", "enableTestCommands=1",
	}
}

func (r *ReplicaSet) initiateLocked(ctx context.Context) error {
	client, err := connectDirect(ctx, r.members[0].address)
	if err != nil {
		return fmt.Errorf("connect for replSetInitiate: %w", err)
	}
	defer disconnectClient(ctx, client)
	members := make(bson.A, len(r.members))
	for index, member := range r.members {
		members[index] = bson.D{{Key: "_id", Value: index}, {Key: "host", Value: member.address}}
	}
	command := bson.D{{Key: "replSetInitiate", Value: bson.D{{Key: "_id", Value: r.name}, {Key: "members", Value: members}}}}
	if err = client.Database("admin").RunCommand(ctx, command).Err(); err != nil {
		return fmt.Errorf("run replSetInitiate: %w", err)
	}
	addresses := make([]string, len(r.members))
	for index, member := range r.members {
		addresses[index] = member.address
	}
	r.URI = "mongodb://" + strings.Join(addresses, ",") + "/?replicaSet=" + r.name + "&retryWrites=true&w=majority"
	return nil
}

func (r *ReplicaSet) waitForMemberLocked(ctx context.Context, member *member) error {
	return waitFor(ctx, func() (bool, error) {
		if err := r.exitedLocked(member); err != nil {
			return false, err
		}
		client, err := connectDirect(ctx, member.address)
		if err != nil {
			return false, nil
		}
		defer disconnectClient(ctx, client)
		if err = client.Ping(ctx, readpref.PrimaryPreferred()); err != nil {
			return false, nil
		}
		return true, nil
	})
}

func (r *ReplicaSet) waitForReplicaLocked(ctx context.Context, wanted int) error {
	return waitFor(ctx, func() (bool, error) {
		primary, ready, err := r.replicaReadyCounts(ctx)
		if err != nil {
			return false, err
		}
		return primary && ready >= wanted, nil
	})
}

func (r *ReplicaSet) replicaReadyCounts(ctx context.Context) (primary bool, ready int, err error) {
	for _, member := range r.members {
		if member.cmd == nil {
			continue
		}
		if err = r.exitedLocked(member); err != nil {
			return false, 0, err
		}
		isPrimary, isReady, stateErr := replicaMemberState(ctx, member.address)
		if stateErr != nil {
			continue
		}
		if isPrimary {
			primary = true
		}
		if isReady {
			ready++
		}
	}
	return primary, ready, nil
}

func replicaMemberState(ctx context.Context, address string) (primary, ready bool, err error) {
	state, err := memberState(ctx, address)
	if err != nil {
		return false, false, err
	}
	switch state {
	case 1:
		return true, true, nil
	case 2:
		return false, true, nil
	default:
		return false, false, nil
	}
}

func (r *ReplicaSet) stopMemberLocked(ctx context.Context, member *member) error {
	if member.cmd == nil {
		return nil
	}
	if err := member.cmd.Process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("interrupt mongod at %s: %w", member.address, err)
	}
	select {
	case err := <-member.wait:
		member.cmd = nil
		member.wait = nil
		if err != nil {
			return fmt.Errorf("mongod at %s stopped: %w", member.address, err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for mongod at %s to stop: %w", member.address, ctx.Err())
	}
}

func (r *ReplicaSet) stopForCleanupLocked(ctx context.Context, member *member) error {
	if member.cmd == nil {
		return nil
	}
	if err := member.cmd.Process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("interrupt mongod at %s during cleanup: %w", member.address, err)
	}
	gracefulCtx, gracefulCancel := context.WithTimeout(ctx, gracefulStopWait)
	defer gracefulCancel()
	select {
	case waitErr := <-member.wait:
		member.cmd = nil
		member.wait = nil
		if waitErr != nil {
			return fmt.Errorf("mongod at %s stopped during cleanup: %w", member.address, waitErr)
		}
		return nil
	case <-gracefulCtx.Done():
	}
	if killErr := member.cmd.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
		return fmt.Errorf("kill mongod at %s: %w", member.address, killErr)
	}
	waitCtx, waitCancel := context.WithTimeout(ctx, shutdownTimeout)
	defer waitCancel()
	select {
	case <-member.wait:
		member.cmd = nil
		member.wait = nil
		return nil // Forced termination is an expected cleanup outcome.
	case <-waitCtx.Done():
		return fmt.Errorf("wait for killed mongod at %s: %w", member.address, waitCtx.Err())
	}
}

func (r *ReplicaSet) exitedLocked(member *member) error {
	if member.wait == nil {
		return nil
	}
	select {
	case err := <-member.wait:
		member.cmd = nil
		member.wait = nil
		if err == nil {
			return fmt.Errorf("%w: %s", errMongodExited, member.address)
		}
		return fmt.Errorf("%w at %s: %w", errMongodExited, member.address, err)
	default:
		return nil
	}
}

func (r *ReplicaSet) memberAt(index int) error {
	if index < 0 || index >= len(r.members) {
		return fmt.Errorf("%w: %d", errMemberIndex, index)
	}
	return nil
}

func (r *ReplicaSet) cleanup(t testing.TB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Client != nil {
		if err := r.Client.Disconnect(ctx); err != nil {
			t.Errorf("disconnect disposable MongoDB replica-set client: %v", err)
		}
		r.Client = nil
	}
	for index := len(r.members) - 1; index >= 0; index-- {
		if err := r.stopForCleanupLocked(ctx, r.members[index]); err != nil {
			t.Errorf("stop disposable MongoDB member %d: %v", index, err)
		}
	}
}

func resolveMongod() (string, error) {
	binary := os.Getenv("MONGOD_BIN")
	if binary == "" {
		binary = "mongod"
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return "", err
	}
	return path, nil
}

func reserveLoopbackAddresses(count int) ([]string, error) {
	listeners := make([]net.Listener, 0, count)
	for range count {
		listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
		if err != nil {
			for _, reserved := range listeners {
				_ = reserved.Close()
			}
			return nil, err
		}
		listeners = append(listeners, listener)
	}
	addresses := make([]string, len(listeners))
	for index, listener := range listeners {
		addresses[index] = listener.Addr().String()
		if err := listener.Close(); err != nil {
			for _, reserved := range listeners[index+1:] {
				_ = reserved.Close()
			}
			return nil, err
		}
	}
	return addresses, nil
}

func connectDirect(ctx context.Context, address string) (*mongo.Client, error) {
	client, err := mongo.Connect(options.Client().ApplyURI("mongodb://" + address + "/?directConnection=true").SetServerSelectionTimeout(connectionTimeout).SetConnectTimeout(connectionTimeout))
	if err != nil {
		return nil, err
	}
	if err = client.Ping(ctx, readpref.PrimaryPreferred()); err != nil {
		disconnectClient(ctx, client)
		return nil, err
	}
	return client, nil
}

func memberState(ctx context.Context, address string) (int32, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, connectionTimeout)
	defer cancel()
	client, err := connectDirect(attemptCtx, address)
	if err != nil {
		return 0, err
	}
	defer disconnectClient(attemptCtx, client)
	var status struct {
		MyState int32 `bson:"myState"`
	}
	if err = client.Database("admin").RunCommand(attemptCtx, bson.D{{Key: "replSetGetStatus", Value: 1}}).Decode(&status); err != nil {
		return 0, err
	}
	return status.MyState, nil
}

func waitFor(ctx context.Context, ready func() (bool, error)) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		ok, err := ready()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func disconnectClient(ctx context.Context, client *mongo.Client) {
	disconnectCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), connectionTimeout)
	defer cancel()
	_ = client.Disconnect(disconnectCtx)
}
