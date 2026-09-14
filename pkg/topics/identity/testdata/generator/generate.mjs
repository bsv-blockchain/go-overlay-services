#!/usr/bin/env node

// Generates portable identity TopicManager fixtures from the pinned TypeScript
// SDK. Run with TS_STACK_ROOT=/path/to/ts-stack and
// C01_TS_STACK_ROOT=/path/to/c01-source, or pass --ts-root/--c01-root.

import { createHash } from 'node:crypto'
import { execFileSync } from 'node:child_process'
import { readFile, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const tsStackCommit = '2bc799a8d8e535242e6de2d305f426ce3975ea7b'
const c01FixtureCommit = 'c58f81787e7c2a985a167c15db9fd9ca5fed6c35'
const args = process.argv.slice(2)
const rootFlag = args.indexOf('--ts-root')
const tsRoot = rootFlag >= 0 ? args[rootFlag + 1] : process.env.TS_STACK_ROOT
const c01RootFlag = args.indexOf('--c01-root')
const c01Root = c01RootFlag >= 0 ? args[c01RootFlag + 1] : process.env.C01_TS_STACK_ROOT
const outputFlag = args.indexOf('--output')
const outputPath = outputFlag >= 0
  ? path.resolve(args[outputFlag + 1])
  : path.resolve(here, '..', 'data', 'identity-topic-fixtures.json')

if (!tsRoot) throw new Error('missing TypeScript stack root (use --ts-root or TS_STACK_ROOT)')
if (!c01Root) throw new Error('missing C01 fixture root (use --c01-root or C01_TS_STACK_ROOT)')

function sha256(bytes) {
  return createHash('sha256').update(bytes).digest()
}

function sha256Hex(bytes) {
  return createHash('sha256').update(bytes).digest('hex')
}

function gitHead(root) {
  return execFileSync('git', ['-C', root, 'rev-parse', 'HEAD'], { encoding: 'utf8' }).trim()
}

function gitShow(root, revision, relativePath) {
  return execFileSync('git', ['-C', root, 'show', `${revision}:${relativePath}`])
}

const tsRootHead = gitHead(tsRoot)
const c01RootHead = gitHead(c01Root)
if (tsRootHead !== tsStackCommit) throw new Error(`TS stack HEAD ${tsRootHead} is not pinned ${tsStackCommit}`)
if (c01RootHead !== c01FixtureCommit) throw new Error(`C01 fixture HEAD ${c01RootHead} is not pinned ${c01FixtureCommit}`)
const artifactRelativePaths = {
  sdkESMMod: 'packages/sdk/dist/esm/mod.js',
  identityTopicManagerESM: 'packages/overlays/topics/dist/identity/IdentityTopicManager.js',
  sdkCertificateESM: 'packages/sdk/dist/esm/src/auth/certificates/Certificate.js',
  sdkVerifiableCertificateESM: 'packages/sdk/dist/esm/src/auth/certificates/VerifiableCertificate.js',
  sdkMasterCertificateESM: 'packages/sdk/dist/esm/src/auth/certificates/MasterCertificate.js',
  sdkProtoWalletESM: 'packages/sdk/dist/esm/src/wallet/ProtoWallet.js',
  sdkPushDropESM: 'packages/sdk/dist/esm/src/script/templates/PushDrop.js',
  sdkTransactionESM: 'packages/sdk/dist/esm/src/transaction/Transaction.js',
  sdkMerklePathESM: 'packages/sdk/dist/esm/src/transaction/MerklePath.js',
  sdkUtilsESM: 'packages/sdk/dist/esm/src/primitives/utils.js'
}
const artifactHashes = Object.fromEntries(
  await Promise.all(Object.entries(artifactRelativePaths).map(async ([name, relativePath]) => {
    return [name, sha256Hex(await readFile(path.join(tsRoot, relativePath)))]
  }))
)
const sourceRelativePaths = {
  sdkCertificateTS: 'packages/sdk/src/auth/certificates/Certificate.ts',
  sdkVerifiableCertificateTS: 'packages/sdk/src/auth/certificates/VerifiableCertificate.ts',
  sdkMasterCertificateTS: 'packages/sdk/src/auth/certificates/MasterCertificate.ts',
  sdkProtoWalletTS: 'packages/sdk/src/wallet/ProtoWallet.ts',
  sdkPushDropTS: 'packages/sdk/src/script/templates/PushDrop.ts',
  sdkTransactionTS: 'packages/sdk/src/transaction/Transaction.ts',
  sdkMerklePathTS: 'packages/sdk/src/transaction/MerklePath.ts',
  sdkUtilsTS: 'packages/sdk/src/primitives/utils.ts',
  identityTopicManagerTS: 'packages/overlays/topics/src/identity/IdentityTopicManager.ts',
  c01FixtureTS: 'packages/wallet/wallet-toolbox/src/utility/__tests__/identityVerification.fixtures.ts',
  c01FixtureJSON: 'packages/wallet/wallet-toolbox/src/utility/__tests__/fixtures/identity-verification.json'
}
const sourceHashes = Object.fromEntries(
  Object.entries(sourceRelativePaths).map(([name, relativePath]) => {
    const root = name.startsWith('c01') ? c01Root : tsRoot
    const revision = name.startsWith('c01') ? c01FixtureCommit : tsStackCommit
    return [name, sha256Hex(gitShow(root, revision, relativePath))]
  })
)

const sdk = await import(pathToFileURL(path.join(tsRoot, 'packages/sdk/dist/esm/mod.js')).href)
const { Certificate, MerklePath, MasterCertificate, P2PKH, PrivateKey, ProtoWallet, PushDrop, Script, Transaction, Utils, VerifiableCertificate } = sdk
const { default: IdentityTopicManager } = await import(
  pathToFileURL(path.join(tsRoot, 'packages/overlays/topics/dist/identity/IdentityTopicManager.js')).href
)

const sourceFixturePath = path.join(
  c01Root,
  'packages/wallet/wallet-toolbox/src/utility/__tests__/fixtures/identity-verification.json'
)
const sourceFixtureSourcePath = path.join(
  c01Root,
  'packages/wallet/wallet-toolbox/src/utility/__tests__/identityVerification.fixtures.ts'
)
if (sha256Hex(await readFile(sourceFixturePath)) !== sourceHashes.c01FixtureJSON) {
  throw new Error('working-tree C01 JSON fixture does not match pinned git source')
}
if (sha256Hex(await readFile(sourceFixtureSourcePath)) !== sourceHashes.c01FixtureTS) {
  throw new Error('working-tree C01 TypeScript fixture does not match pinned git source')
}
const sourceFixture = JSON.parse(await readFile(sourceFixturePath, 'utf8'))
const protocolID = [1, 'identity']
const keyID = '1'
const confirmedHeight = 700_000

function sha256d(bytes) {
  return sha256(sha256(bytes))
}

function txidFromBytes(bytes) {
  return Buffer.from(sha256d(bytes)).reverse().toString('hex')
}

function merkleRoot(left, right) {
  const leftLE = Buffer.from(left, 'hex').reverse()
  const rightLE = Buffer.from(right, 'hex').reverse()
  return Buffer.from(sha256d(Buffer.concat([leftLE, rightLE]))).reverse().toString('hex')
}

function confirm(tx) {
  const txid = tx.id('hex')
  if (txidFromBytes(tx.toUint8Array()) !== txid) throw new Error('SDK transaction ID disagrees with SHA-256d')
  const path = new MerklePath(confirmedHeight, [[
    { offset: 0, hash: '42'.repeat(32) },
    { offset: 1, hash: txid, txid: true }
  ]])
  tx.merklePath = path
  const root = merkleRoot('42'.repeat(32), txid)
  if (path.computeRoot(txid) !== root) throw new Error('SDK Merkle root disagrees with SHA-256d')
  return { txid, merkleRoot: root }
}

function decodeCertificate(lockingScript) {
  const decoded = PushDrop.decode(lockingScript)
  return JSON.parse(Utils.toUTF8(decoded.fields[0]))
}

function certificatePreimage(certificate, compare) {
  const writer = new Utils.Writer()
  writer.write(Utils.toArray(certificate.type, 'base64'))
  writer.write(Utils.toArray(certificate.serialNumber, 'base64'))
  writer.write(Utils.toArray(certificate.subject, 'hex'))
  writer.write(Utils.toArray(certificate.certifier, 'hex'))
  const [revocationTXID, revocationOutputIndex] = certificate.revocationOutpoint.split('.')
  writer.write(Utils.toArray(revocationTXID, 'hex'))
  writer.writeVarIntNum(Number(revocationOutputIndex))
  const fieldNames = Object.keys(certificate.fields).sort(compare)
  writer.writeVarIntNum(fieldNames.length)
  for (const fieldName of fieldNames) {
    const fieldNameBytes = Utils.toArray(fieldName, 'utf8')
    const fieldValueBytes = Utils.toArray(certificate.fields[fieldName], 'utf8')
    writer.writeVarIntNum(fieldNameBytes.length)
    writer.write(fieldNameBytes)
    writer.writeVarIntNum(fieldValueBytes.length)
    writer.write(fieldValueBytes)
  }
  return writer.toArray()
}

function tsLocaleCompare(a, b) {
  return a.localeCompare(b)
}

function utf8ByteCompare(a, b) {
  return Buffer.from(a, 'utf8').compare(Buffer.from(b, 'utf8'))
}

function certificateWithKeyring(certificate, keyring) {
  return new VerifiableCertificate(
    certificate.type,
    certificate.serialNumber,
    certificate.subject,
    certificate.certifier,
    certificate.revocationOutpoint,
    certificate.fields,
    keyring,
    certificate.signature
  )
}

async function lockCertificate(certificate, sign = true) {
  const certificateJSON = Utils.toArray(JSON.stringify(certificate), 'utf8')
  return await new PushDrop(subjectWallet).lock(
    [certificateJSON],
    protocolID,
    keyID,
    'anyone',
    true,
    sign
  )
}

function caseInfo(index, name, valid) {
  return { index, name, valid, reason: valid ? 'valid signed identity certificate' : `rejected ${name}` }
}

function atomicEncoding(tx) {
  const atomic = tx.toAtomicBEEF()
  return {
    atomicBEEFBase64: Buffer.from(atomic).toString('base64'),
    atomicBEEFSha256: sha256Hex(Buffer.from(atomic))
  }
}

const sourceBytes = Buffer.from(sourceFixture.certificateBEEF, 'base64')
if (sha256Hex(sourceBytes) !== sourceFixture.certificateBEEFSha256) {
  throw new Error('pinned C01 certificate BEEF digest mismatch')
}
const sourceCertificateTransaction = Transaction.fromBEEF([...sourceBytes])
const sourceCertificateScript = sourceCertificateTransaction.outputs[0].lockingScript
const c01Certificate = decodeCertificate(sourceCertificateScript)
const subject = new PrivateKey(11)
const certifier = new PrivateKey(12)
const subjectWallet = new ProtoWallet(subject)
const certifierWallet = new ProtoWallet(certifier)
const anyoneWallet = new ProtoWallet('anyone')

// Reproduce the C01 confirmed signed spend. Its input spends a separately
// confirmed P2PKH ancestor, so this is suitable for graph/SPV tests as well.
const spendingKey = new PrivateKey(13)
const ancestor = new Transaction()
ancestor.addInput({
  sourceTXID: '00'.repeat(32),
  sourceOutputIndex: 0,
  unlockingScript: Script.fromASM('OP_TRUE')
})
ancestor.addOutput({ satoshis: 10, lockingScript: new P2PKH().lock(spendingKey.toAddress()) })
const ancestorProof = confirm(ancestor)

async function spend(lockingScripts) {
  const tx = new Transaction()
  tx.addInput({
    sourceTransaction: ancestor,
    sourceOutputIndex: 0,
    unlockingScriptTemplate: new P2PKH().unlock(spendingKey)
  })
  for (const lockingScript of lockingScripts) tx.addOutput({ satoshis: 1, lockingScript })
  await tx.sign()
  return tx
}

const c01Spend = await spend([sourceCertificateScript])
const c01Proof = confirm(c01Spend)
const manager = new IdentityTopicManager()

// Build a second certificate with field names that exercise the TS
// localeCompare ordering used by Certificate.toBinary. The fields are
// encrypted by the certifier, signed by the certifier, and revealed to the
// anyone wallet through a real public keyring.
const mixedPlaintextFields = {
  Name: 'Alice',
  name: 'lowercase',
  éclair: 'choux',
  zebra: 'zebra-value',
  userName: 'alice-user'
}
const mixedType = Utils.toBase64(Array(32).fill(7))
const mixedSerialNumber = Utils.toBase64(Array(32).fill(8))
const mixedRevocationOutpoint = `${'11'.repeat(32)}.0`
const mixedEncrypted = await MasterCertificate.createCertificateFields(
  certifierWallet,
  subject.toPublicKey().toString(),
  mixedPlaintextFields
)
const mixedSignedCertificate = new Certificate(
  mixedType,
  mixedSerialNumber,
  subject.toPublicKey().toString(),
  certifier.toPublicKey().toString(),
  mixedRevocationOutpoint,
  mixedEncrypted.certificateFields
)
await mixedSignedCertificate.sign(certifierWallet)
if (!(await mixedSignedCertificate.verify())) throw new Error('mixed certificate signature did not verify')
const anyonePublicKey = (await anyoneWallet.getPublicKey({ identityKey: true })).publicKey
const mixedPublicKeyring = await MasterCertificate.createKeyringForVerifier(
  subjectWallet,
  certifier.toPublicKey().toString(),
  anyonePublicKey,
  mixedSignedCertificate.fields,
  Object.keys(mixedPlaintextFields),
  mixedEncrypted.masterKeyring,
  mixedSignedCertificate.serialNumber
)
const mixedCertificate = new VerifiableCertificate(
  mixedSignedCertificate.type,
  mixedSignedCertificate.serialNumber,
  mixedSignedCertificate.subject,
  mixedSignedCertificate.certifier,
  mixedSignedCertificate.revocationOutpoint,
  mixedSignedCertificate.fields,
  mixedPublicKeyring,
  mixedSignedCertificate.signature
)
const mixedDecrypted = await mixedCertificate.decryptFields(anyoneWallet)
for (const [name, value] of Object.entries(mixedPlaintextFields)) {
  if (mixedDecrypted[name] !== value) throw new Error(`mixed certificate field failed to decrypt: ${name}`)
}
const mixedCertificateJSON = JSON.parse(JSON.stringify(mixedCertificate))
const mixedCertificateScript = await lockCertificate(mixedCertificateJSON)

async function createOrderingProfile(name, fieldEntries, profileNumber) {
  const plainFields = Object.fromEntries(fieldEntries)
  const encrypted = await MasterCertificate.createCertificateFields(
    certifierWallet,
    subject.toPublicKey().toString(),
    plainFields
  )
  const certificate = new Certificate(
    Utils.toBase64(Array(32).fill(20 + profileNumber)),
    Utils.toBase64(Array(32).fill(40 + profileNumber)),
    subject.toPublicKey().toString(),
    certifier.toPublicKey().toString(),
    `${(60 + profileNumber).toString(16).padStart(2, '0').repeat(32)}.0`,
    encrypted.certificateFields
  )
  await certificate.sign(certifierWallet)
  const publicKeyring = await MasterCertificate.createKeyringForVerifier(
    subjectWallet,
    certifier.toPublicKey().toString(),
    anyonePublicKey,
    certificate.fields,
    Object.keys(plainFields),
    encrypted.masterKeyring,
    certificate.serialNumber
  )
  const tsVerifiable = certificateWithKeyring(certificate, publicKeyring)
  const tsJSON = JSON.parse(JSON.stringify(tsVerifiable))
  const tsEnvelopeScript = await lockCertificate(tsJSON)
  const tsEnvelopeJSONBytes = Utils.toArray(JSON.stringify(tsJSON), 'utf8')

  const standardSortOrder = Object.keys(certificate.fields).sort()
  const tsLocaleCompareOrder = Object.keys(certificate.fields).sort(tsLocaleCompare)
  const goByteOrder = Object.keys(certificate.fields).sort(utf8ByteCompare)
  const tsPreimage = certificate.toBinary(false)
  const utf8Preimage = certificatePreimage(certificate, utf8ByteCompare)
  const brcSignature = await certifierWallet.createSignature({
    data: utf8Preimage,
    protocolID: [2, 'certificate signature'],
    keyID: `${certificate.type} ${certificate.serialNumber}`
  })
  const brcCertificate = new Certificate(
    certificate.type,
    certificate.serialNumber,
    certificate.subject,
    certificate.certifier,
    certificate.revocationOutpoint,
    certificate.fields,
    Utils.toHex(brcSignature.signature)
  )
  const brcVerifiable = certificateWithKeyring(brcCertificate, publicKeyring)
  const brcJSON = JSON.parse(JSON.stringify(brcVerifiable))
  const brcEnvelopeScript = await lockCertificate(brcJSON)
  const tsVerify = await tsVerifiable.verify()
  let brcVerify = false
  try {
    brcVerify = await brcVerifiable.verify()
  } catch {
    brcVerify = false
  }
  const decrypted = await tsVerifiable.decryptFields(anyoneWallet)
  for (const [fieldName, fieldValue] of Object.entries(plainFields)) {
    if (decrypted[fieldName] !== fieldValue) throw new Error(`ordering profile field failed to decrypt: ${fieldName}`)
  }
  const tsProfileSpend = await spend([tsEnvelopeScript])
  const brcProfileSpend = await spend([brcEnvelopeScript])
  const tsProfileBeef = tsProfileSpend.toBEEF()
  const brcProfileBeef = brcProfileSpend.toBEEF()
  const tsOutputsToAdmit = await admission(tsProfileBeef)
  const utf8OutputsToAdmit = await admission(brcProfileBeef)
  if (tsOutputsToAdmit.join(',') !== '0') throw new Error(`unexpected TS admission for ${name}: ${tsOutputsToAdmit}`)
  const expectedUTF8Admission = tsPreimage.join(',') === utf8Preimage.join(',') ? '0' : ''
  if (utf8OutputsToAdmit.join(',') !== expectedUTF8Admission) {
    throw new Error(`unexpected UTF-8 comparator admission for ${name}: ${utf8OutputsToAdmit}`)
  }
  return {
    name,
    inputFieldOrder: fieldEntries.map(([fieldName]) => fieldName),
    expectedFields: plainFields,
    fieldOrders: {
      tsLocaleCompare: tsLocaleCompareOrder,
      javascriptDefaultSortUTF16: standardSortOrder,
      goUTF8ByteOrder: goByteOrder
    },
    certificate: tsJSON,
    certificateJSONFieldOrder: Object.keys(tsJSON.fields),
    certificateToBinaryWithoutSignatureBase64: Buffer.from(tsPreimage).toString('base64'),
    utf8ComparatorCertificatePreimageBase64: Buffer.from(utf8Preimage).toString('base64'),
    envelopeJSONBase64: Buffer.from(tsEnvelopeJSONBytes).toString('base64'),
    envelopeJSONSha256: sha256Hex(Buffer.from(tsEnvelopeJSONBytes)),
    tsCertificateVerify: tsVerify,
    utf8ComparatorCertificate: brcJSON,
    utf8ComparatorCertificateVerifyUnderTS: brcVerify,
    tsTopicManager: {
      beefBase64: Buffer.from(tsProfileBeef).toString('base64'),
      beefSha256: sha256Hex(Buffer.from(tsProfileBeef)),
      txid: tsProfileSpend.id('hex'),
      ...atomicEncoding(tsProfileSpend),
      outputsToAdmit: tsOutputsToAdmit
    },
    utf8ComparatorTopicManager: {
      beefBase64: Buffer.from(brcProfileBeef).toString('base64'),
      beefSha256: sha256Hex(Buffer.from(brcProfileBeef)),
      txid: brcProfileSpend.id('hex'),
      ...atomicEncoding(brcProfileSpend),
      outputsToAdmit: utf8OutputsToAdmit
    }
  }
}

async function createSearchableProfile() {
  const plainFields = {
    name: 'Alice Smith',
    profilePhoto: 'https://example.test/private-photo',
    icon: 'avatar-icon',
    userName: 'Alice',
    '2': 'second',
    '10': 'tenth',
    bom: '\ufeffAlice BOM'
  }
  const encrypted = await MasterCertificate.createCertificateFields(
    certifierWallet,
    subject.toPublicKey().toString(),
    plainFields
  )
  const certificate = new Certificate(
    Utils.toBase64(Array(32).fill(31)),
    Utils.toBase64(Array(32).fill(51)),
    subject.toPublicKey().toString(),
    certifier.toPublicKey().toString(),
    `${'91'.repeat(32)}.0`,
    encrypted.certificateFields
  )
  await certificate.sign(certifierWallet)
  const publicKeyring = await MasterCertificate.createKeyringForVerifier(
    subjectWallet,
    certifier.toPublicKey().toString(),
    anyonePublicKey,
    certificate.fields,
    Object.keys(plainFields),
    encrypted.masterKeyring,
    certificate.serialNumber
  )
  const verifiable = certificateWithKeyring(certificate, publicKeyring)
  const certificateJSON = JSON.parse(JSON.stringify(verifiable))
  const envelopeScript = await lockCertificate(certificateJSON)
  const decrypted = await verifiable.decryptFields(anyoneWallet)
  if (decrypted.bom !== 'Alice BOM') throw new Error(`unexpected BOM decoding: ${JSON.stringify(decrypted.bom)}`)
  const decryptedFieldIterationOrder = Object.keys(decrypted)
  const searchableFieldNames = decryptedFieldIterationOrder.filter(fieldName => !['profilePhoto', 'icon'].includes(fieldName))
  const searchableAttributes = searchableFieldNames.map(fieldName => ({ name: fieldName, value: decrypted[fieldName] }))
  const searchableConcatenated = searchableAttributes.map(attribute => attribute.value).join(' ')
  const profileSpend = await spend([envelopeScript])
  const profileBEEF = profileSpend.toBEEF()
  const outputsToAdmit = await admission(profileBEEF)
  if (outputsToAdmit.join(',') !== '0') throw new Error(`unexpected searchable profile admission: ${outputsToAdmit}`)
  return {
    name: 'searchable-fields',
    sourceFields: plainFields,
    expectedFields: decrypted,
    certificate: certificateJSON,
    certificateFieldOrderTsLocaleCompare: Object.keys(certificate.fields).sort(tsLocaleCompare),
    certificateToBinaryWithoutSignatureBase64: Buffer.from(certificate.toBinary(false)).toString('base64'),
    decryptedFieldIterationOrder,
    searchableFieldNames,
    searchableAttributes,
    searchableConcatenated,
    beefBase64: Buffer.from(profileBEEF).toString('base64'),
    beefSha256: sha256Hex(Buffer.from(profileBEEF)),
    txid: profileSpend.id('hex'),
    ...atomicEncoding(profileSpend),
    outputsToAdmit
  }
}

// Each malformed output changes one verification stage while preserving the
// surrounding transaction and the other identity primitives.
const malformedEnvelopeScript = Script.fromASM('OP_FALSE OP_RETURN')
const invalidEnvelopeSignatureScript = await lockCertificate(c01Certificate)
if (invalidEnvelopeSignatureScript.chunks[3]?.data == null) throw new Error('envelope signature chunk missing')
invalidEnvelopeSignatureScript.chunks[3].data = [...invalidEnvelopeSignatureScript.chunks[3].data]
invalidEnvelopeSignatureScript.chunks[3].data[0] ^= 1
const invalidCertificate = { ...c01Certificate, signature: '00'.repeat(64) }
const invalidCertificateSignatureScript = await lockCertificate(invalidCertificate)
const undecryptableCertificate = { ...c01Certificate, keyring: {} }
const undecryptableScript = await lockCertificate(undecryptableCertificate)

const orderingProfiles = [
  await createOrderingProfile('ascii-case', [
    ['Name', 'Alice'],
    ['name', 'lowercase'],
    ['userName', 'alice-user'],
    ['zebra', 'zebra-value']
  ], 1),
  await createOrderingProfile('accent-combining-forward', [
    ['é', 'precomposed'],
    ['e\u0301', 'combining'],
    ['alpha', 'alpha-value']
  ], 2),
  await createOrderingProfile('accent-combining-reverse', [
    ['e\u0301', 'combining'],
    ['é', 'precomposed'],
    ['alpha', 'alpha-value']
  ], 3),
  await createOrderingProfile('astral-versus-bmp', [
    ['𐀀', 'astral-value'],
    ['\uE000', 'bmp-private-use'],
    ['alpha', 'alpha-value']
  ], 4)
]
const searchableProfile = await createSearchableProfile()

const mixedOutputs = [
  sourceCertificateScript,
  malformedEnvelopeScript,
  invalidEnvelopeSignatureScript,
  invalidCertificateSignatureScript,
  undecryptableScript,
  mixedCertificateScript,
  sourceCertificateScript
]
const mixedSpend = await spend(mixedOutputs)
const mixedProof = confirm(mixedSpend)

async function admission(beef) {
  const previousLog = console.log
  const previousError = console.error
  console.log = () => {}
  console.error = () => {}
  try {
    const result = await manager.identifyAdmissibleOutputs(beef, [])
    return result.outputsToAdmit
  } finally {
    console.log = previousLog
    console.error = previousError
  }
}

const c01BEEF = c01Spend.toBEEF()
const mixedBEEF = mixedSpend.toBEEF()
const c01Base64 = Buffer.from(c01BEEF).toString('base64')
const mixedBase64 = Buffer.from(mixedBEEF).toString('base64')
const expectedC01 = await admission(c01BEEF)
const expectedMixed = await admission(mixedBEEF)
if (expectedC01.join(',') !== '0') throw new Error(`unexpected C01 admission: ${expectedC01}`)
if (expectedMixed.join(',') !== '0,5,6') throw new Error(`unexpected mixed admission: ${expectedMixed}`)

const corpus = {
  schemaVersion: 1,
  topic: 'tm_identity',
  protocolID,
  keyID,
  confirmedHeight,
  source: {
    tsStackCommit,
    c01FixtureCommit,
    tsStackHead: tsRootHead,
    c01FixtureHead: c01RootHead,
    c01FixturePath: 'packages/wallet/wallet-toolbox/src/utility/__tests__/identityVerification.fixtures.ts',
    c01CertificateFixturePath: 'packages/wallet/wallet-toolbox/src/utility/__tests__/fixtures/identity-verification.json',
    c01CertificateBEEFSha256: sourceFixture.certificateBEEFSha256,
    c01CertificateBEEFBase64: sourceFixture.certificateBEEF,
    sourceGitObjectSHA256: sourceHashes,
    builtArtifactSHA256: artifactHashes,
    generator: 'pkg/topics/identity/testdata/generator/generate.mjs'
  },
  runtime: {
    node: process.version,
    v8: process.versions.v8,
    icu: process.versions.icu,
    unicode: process.versions.unicode,
    collator: new Intl.Collator().resolvedOptions()
  },
  certificates: {
    c01: c01Certificate,
    mixed: mixedCertificateJSON,
    mixedFieldOrderTsLocaleCompare: Object.keys(mixedCertificate.fields).sort((a, b) => a.localeCompare(b))
  },
  transport: {
    identityEnvelope: 'PushDrop carries the certificate as a UTF-8 JSON field; the certificate signing preimage is not transported.',
    certificateSigningPreimage: 'Certificate.toBinary(false) sorts field names with JavaScript localeCompare before Certificate.sign.',
    diagnosticPreimages: 'Profiles emit both the TS localeCompare preimage and an independently signed UTF-8 byte-order comparator preimage.'
  },
  orderingProfiles,
  searchableProfile,
  fixtures: [
    {
      name: 'c01-confirmed-signed-spend',
      description: 'C01 certificate PushDrop output in an actual signed spend with a two-leaf confirmation proof.',
      beefBase64: c01Base64,
      beefSha256: sha256Hex(Buffer.from(c01BEEF)),
      ...atomicEncoding(c01Spend),
      txid: c01Proof.txid,
      merkleRoot: c01Proof.merkleRoot,
      ancestorTxid: ancestorProof.txid,
      outputs: [caseInfo(0, 'c01-valid', true)],
      outputsToAdmit: expectedC01
    },
    {
      name: 'mixed-valid-and-malformed-outputs',
      description: 'One signed spend containing three valid outputs and four individually malformed identity outputs.',
      beefBase64: mixedBase64,
      beefSha256: sha256Hex(Buffer.from(mixedBEEF)),
      ...atomicEncoding(mixedSpend),
      txid: mixedProof.txid,
      merkleRoot: mixedProof.merkleRoot,
      ancestorTxid: ancestorProof.txid,
      outputs: [
        caseInfo(0, 'c01-valid', true),
        caseInfo(1, 'malformed-envelope', false),
        caseInfo(2, 'invalid-envelope-signature', false),
        caseInfo(3, 'invalid-certificate-signature', false),
        caseInfo(4, 'undecryptable-certificate', false),
        caseInfo(5, 'mixed-fields-valid', true),
        caseInfo(6, 'c01-valid-duplicate', true)
      ],
      outputsToAdmit: expectedMixed,
      expectedFields: mixedPlaintextFields
    }
  ]
}

await writeFile(outputPath, `${JSON.stringify(corpus, null, 2)}\n`)
console.log(`wrote ${outputPath}`)
