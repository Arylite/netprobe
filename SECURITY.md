# Security

## Reporting a vulnerability

Please do not open a public issue. Report it privately through GitHub:
**Security > Report a vulnerability** on the repository. Say what you found, how to
reproduce it, and which version. You get an answer within a few days, and a fix
or a date for one as soon as the problem is understood.

Only the latest release is supported.

## What is protected, and how

The README describes the model in its *Security model* section. In short:

- Edges and the UI authenticate with secrets that are stored hashed (tokens) or
  slow-hashed (passwords, argon2id); the secrets that must be read back (the
  address and signing secret of a webhook) are encrypted with a key kept in a file.
- Both APIs can serve TLS 1.3 themselves, with certificates reloaded without a
  restart, and with client certificates required of edges if you want them.
- What an edge probes and what the central calls are limited by deny lists applied
  after name resolution.
- Sign-ins and every change are written to an audit trail.
- The images run without privileges, on a read-only file system, and are scanned
  for known vulnerabilities before they are published.

## Releases

Each release carries `SHA256SUMS` and an attestation of where it was built:

```sh
gh attestation verify FILE --repo Arylite/netprobe
```
