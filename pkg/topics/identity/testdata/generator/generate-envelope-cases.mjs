#!/usr/bin/env node

// Exact-property-name regressions from the pinned TS TopicManager and existing
// C01 certificate. No certificate byte regeneration or real chain/database use.
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { execFileSync } from 'node:child_process'
import { readFile, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const root = process.env.TS_STACK_ROOT
if (!root) throw new Error('TS_STACK_ROOT is required')
const corpus = JSON.parse(await readFile(path.join(here, '../data/identity-topic-fixtures.json'), 'utf8'))
const pin = corpus.source.tsStackCommit
assert.equal(execFileSync('git', ['-C', root, 'rev-parse', 'HEAD'], { encoding: 'utf8' }).trim(), pin)
const sha256 = bytes => createHash('sha256').update(bytes).digest('hex')
const artifactPaths = {
  sdkESMMod: 'packages/sdk/dist/esm/mod.js',
  identityTopicManagerESM: 'packages/overlays/topics/dist/identity/IdentityTopicManager.js',
  sdkCertificateESM: 'packages/sdk/dist/esm/src/auth/certificates/Certificate.js',
  sdkVerifiableCertificateESM: 'packages/sdk/dist/esm/src/auth/certificates/VerifiableCertificate.js',
  sdkProtoWalletESM: 'packages/sdk/dist/esm/src/wallet/ProtoWallet.js',
  sdkPushDropESM: 'packages/sdk/dist/esm/src/script/templates/PushDrop.js',
  sdkTransactionESM: 'packages/sdk/dist/esm/src/transaction/Transaction.js',
  sdkUtilsESM: 'packages/sdk/dist/esm/src/primitives/utils.js'
}
// Pin the observed direct artifacts before importing; this is not a hermetic
// rebuild or a checksum of every transitive JS dependency.
for (const [name, relative] of Object.entries(artifactPaths)) {
  assert.equal(sha256(await readFile(path.join(root, relative))), corpus.source.builtArtifactSHA256[name], name)
}
const { PrivateKey, ProtoWallet, PushDrop, Transaction, P2PKH, Utils } = await import(pathToFileURL(path.join(root, artifactPaths.sdkESMMod)))
const { default: IdentityTopicManager } = await import(pathToFileURL(path.join(root, artifactPaths.identityTopicManagerESM)))
const original = Transaction.fromBEEF([...Buffer.from(corpus.fixtures[0].beefBase64, 'base64')])
const envelope = JSON.parse(Utils.toUTF8(PushDrop.decode(original.outputs[0].lockingScript).fields[0]))
const subjectWallet = new ProtoWallet(new PrivateKey(11))
const unconfirmed = Transaction.fromBEEF([...Buffer.from(corpus.searchableProfile.beefBase64, 'base64')])
const ancestor = unconfirmed.inputs[0].sourceTransaction
assert.ok(ancestor, 'the unconfirmed fixture must carry its confirmed ancestor')
assert.equal(ancestor.id('hex'), original.inputs[0].sourceTXID)
const cases = []
const mutations = [
  ['control', value => JSON.stringify(value), [0]],
  ['upper-only-subject', value => { value.SUBJECT = value.subject; delete value.subject; return JSON.stringify(value) }, []],
  ['shadow-type', value => { value.TYPE = 'ignored by TS'; return JSON.stringify(value) }, [0]],
  ['duplicate-null-subject', value => JSON.stringify(value).slice(0, -1) + ',"subject":null}', []],
  ['duplicate-valid-subject-last', value => '{"subject":null,' + JSON.stringify(value).slice(1), [0]],
  ['upper-only-fields', value => { value.FIELDS = value.fields; delete value.fields; return JSON.stringify(value) }, []],
  ['shadow-keyring', value => { value.KEYRING = null; return JSON.stringify(value) }, [0]]
]
for (const [name, mutate, expected] of mutations) {
  const raw = mutate(structuredClone(envelope))
  const lockingScript = await new PushDrop(subjectWallet).lock([Utils.toArray(raw)], [1, 'identity'], '1', 'anyone', true, true)
  const tx = new Transaction()
  tx.addInput({ sourceTransaction: ancestor, sourceOutputIndex: 0, unlockingScriptTemplate: new P2PKH().unlock(new PrivateKey(13)) })
  tx.addOutput({ satoshis: 1, lockingScript })
  await tx.sign()
  const beef = tx.toBEEF()
  const atomic = tx.toAtomicBEEF()
  const admitted = (await new IdentityTopicManager().identifyAdmissibleOutputs(beef, [])).outputsToAdmit
  assert.deepEqual(admitted, expected, name)
  cases.push({ name, scriptHex: lockingScript.toHex(), envelopeJSON: raw, beefBase64: Buffer.from(beef).toString('base64'), beefSha256: sha256(Buffer.from(beef)), atomicBEEFBase64: Buffer.from(atomic).toString('base64'), atomicBEEFSha256: sha256(Buffer.from(atomic)), txid: tx.id('hex'), outputsToAdmit: admitted })
}
await writeFile(path.join(here, '../data/envelope-cases.json'), JSON.stringify({ source: { tsStackCommit: pin, certificateCorpusSHA256: sha256(await readFile(path.join(here, '../data/identity-topic-fixtures.json'))), scope: 'actual TS subject-signed PushDrop outputs in P2PKH-signed unconfirmed spends; topic admission only, no SPV claim' }, cases }, null, 2) + '\n')
console.log(JSON.stringify(cases.map(({ name, outputsToAdmit }) => ({ name, outputsToAdmit }))))
