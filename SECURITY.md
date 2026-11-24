# Security Policy

FlowLens is a local MITM proxy and traffic inspection tool. We welcome security reports—especially issues that could expose captured data, weaken TLS or certificate handling, cross a local trust boundary, execute code unexpectedly, or compromise release artifacts.

## Supported Versions

Security fixes are developed on the `main` branch and shipped in subsequent releases. FlowLens does not currently maintain separate security-support windows for older versions. When safe to do so, verify that an issue reproduces on the latest release or current `main` before reporting it.

## Reporting a Vulnerability

Please use GitHub Private Vulnerability Reporting through the repository's [security advisory form](https://github.com/josexy/flowlens/security/advisories/new) whenever **Report a vulnerability** is available.

Include only the details needed to reproduce and assess the issue:

- Affected version, commit SHA, and operating system.
- Preconditions and the trust boundary crossed.
- Minimal reproduction steps or a compact proof of concept.
- Expected versus observed behavior.
- Likely impact and any known mitigations.

Do not include real credentials, captured traffic, HAR files, databases, API Collections, private keys, certificates, signing material, personal file paths, or unredacted logs. Use synthetic test data and redact secrets even in private reports unless a maintainer explicitly requests a safer transfer channel.

If private vulnerability reporting is unavailable, open a minimal [public issue](https://github.com/josexy/flowlens/issues/new) stating only that a potential vulnerability requires a private reporting channel. Do not include exploit details, affected secrets, or sensitive environment information in that issue.

Please give maintainers reasonable time to reproduce and fix a confirmed issue before public disclosure. There is no guaranteed response-time SLA at present.

## Scope Notes

Examples of in-scope reports include:

- Unauthorized file access or deletion outside FlowLens-managed storage directories.
- Unintended exposure of captures, history, API Collections, logs, certificates, or process metadata.
- TLS verification, CA trust, client-certificate, proxy-boundary, or request-routing flaws.
- Bypassing documented limits for body files, Worker protocol communication, or process isolation.
- Release workflow or artifact integrity weaknesses.
- Vulnerabilities in bundled dependencies that are reachable through FlowLens.

Expected behavior is not a vulnerability by itself: FlowLens intentionally decrypts traffic routed through its trusted CA, can listen on non-loopback interfaces when configured to do so, and stores captured data locally. Python plugins run as trusted user code and are not a security sandbox. Reports demonstrating an unexpected boundary crossing or an unsafe default are always welcome.

Use FlowLens only on traffic and systems you own or are authorized to test.
