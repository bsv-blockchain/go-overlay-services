# BRC-136 BASM test vectors

`vectors.json` contains language-neutral frozen values for the BRC-136
Block-Aligned Sparse Merkle Root and Topic Anchor Chain algorithms. Hash fields
are lowercase display-order hex: reverse each field to internal byte order
before hashing, then reverse the result for display. BASM uses the ordered
admitted list, a zero root for an empty list, singleton identity, double
SHA-256 for parents, and Bitcoin odd-node duplication at every level. TAC is
seeded with 32 zero bytes at `genesisHeight - 1` and hashes the previous TAC,
block hash, and BASM root for every contiguous anchor, including empty roots.
The current BRC-136 text at repository HEAD defines this three-field TAC input;
`admittedCount` is not included in the TAC hash.

The vectors use the Bitcoin genesis transaction ID in the singleton fixture,
synthetic asymmetric byte patterns, and synthetic block hashes. The
three-leaf/repeated-last-four pair is deliberately illustrative of the
Bitcoin duplicate-leaf ambiguity: both roots collide under odd duplication,
but the four-entry list is marked `admissionListValid: false` and must not be
accepted as a real admitted list.

`verify_vectors.py` is a standard-library-only checker (it also requires the
system `openssl` executable for an independent digest cross-check). It
recalculates every root and TAC from the frozen JSON and never writes expected
values. Run it from this directory with:

```sh
python3 verify_vectors.py
```

Provenance: normative text was fetched from the pinned commit
`2733cd2950a739b3c977b95d652ff63e3773c40b` at
`https://github.com/bsv-blockchain/BRCs/blob/2733cd2950a739b3c977b95d652ff63e3773c40b/overlays/0136.md`.
Its tree resolves `overlays/0136.md` to blob
`8b14b70c66ca8ba391995ceb8f84f30b7d46afcc`, the same blob resolved at repo
HEAD `39a643ff148a8dcd23ec08986a8ddeb7d5713743`; both revisions are retained
in `specRevision` in the JSON.
A direct raw-file comparison fetched both revisions as 21,721 bytes and
returned `raw_equal=True`, with SHA-256
`e85678d430d51cb2cd600f8ee745b6b2b01715917d9f0e8868e277f8a82889bc` for each.

Independent calculation evidence (the checker invokes
`openssl dgst -sha256 -binary` twice through `subprocess.run` for every
multi-leaf BASM fixture and every TAC anchor):

```sh
python3 verify_vectors.py
printf '%s' '1f1e1d1c1b1a191817161514131211100f0e0d0c0b0a09080706050403020100efcdab8967452301bebafecaefbeadde00112233445566778899aabbccddeeff' \
  | xxd -r -p | openssl dgst -sha256 -binary | openssl dgst -sha256
python3 -c 'import sys; sys.stdout.buffer.write(bytes(96))' \
  | openssl dgst -sha256 -binary | openssl dgst -sha256
```

Observed output on 2026-09-08 was `verify_vectors.py: OK (...; OpenSSL
cross-checks: 14)`, pair digest
`fccf4c25b31f040b36e646ebf6a2ebaac8dc09fd7c1cd35e69b3183864668066` (the
internal form of the displayed `even-two-asymmetric` root), and genesis TAC
digest `d328800c7f3ccafe648d3eb43b47eb416f48103f1cd81ddd7a0c41431e4e463a`
(the internal form of the first TAC fixture's displayed value). The BASM
parent and TAC double hashes are calculated with Python `hashlib.sha256`
twice and independently with OpenSSL `dgst -sha256` twice, with all
display/internal reversals performed explicitly in the checker.
