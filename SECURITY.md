# Security Policy

FlowLens is a local MITM proxy and traffic inspection tool. Security reports are welcome, especially when an issue could expose captured data, weaken TLS or certificate handling, cross a local trust boundary, execute code unexpectedly, or compromise release artifacts.

## Supported Versions

Security fixes are developed against the current `main` branch and released in a subsequent version. The project does not currently guarantee a separate security-support window for older releases. Before reporting a problem found only in an older build, reproduce it with the latest release or current source when it is safe to do so.

## Reporting a Vulnerability

Prefer GitHub Private Vulnerability Reporting: open the repository's [security advisory form](https://github.com/josexy/flowlens/security/advisories/new) when the **Report a vulnerability** option is available.

Include only what is needed to reproduce and assess the issue:

- affected version, commit, and operating system;
- attack preconditions and trust boundary;
- minimal reproduction steps or a small proof of concept;
- expected and observed behavior;
- likely impact and any known mitigation.

Do not put real credentials, captured traffic, HAR files, databases, API Collections, private keys, certificates, signing material, personal paths, or unredacted logs in a report. Use synthetic data and redact secrets even in a private report unless a maintainer explicitly requests a safer transfer method.

If private vulnerability reporting is unavailable, open a minimal [public issue](https://github.com/josexy/flowlens/issues/new) that says a potential vulnerability needs a private reporting channel. Do not describe the exploit, affected secrets, or sensitive environment details in that issue.

Please allow maintainers time to reproduce and remediate a confirmed issue before public disclosure. There is no guaranteed response-time SLA at present.

## Scope Notes

Examples of useful reports include:

- unauthorized file access or deletion outside FlowLens-managed storage;
- unintended exposure of captures, history, API Collections, logs, certificates, or process metadata;
- TLS verification, CA, client-certificate, proxy-boundary, or request-routing flaws;
- escaping documented limits around body files, Worker communication, or process isolation;
- release workflow or artifact integrity weaknesses;
- vulnerabilities in bundled dependencies that are reachable through FlowLens.

Expected behavior is not by itself a vulnerability: FlowLens intentionally decrypts traffic trusted through its generated CA, can listen on non-loopback interfaces when configured, and stores requested capture data locally. Python plugins are trusted user code and are not a security sandbox. Reports showing an unexpected boundary crossing or an unsafe default are still welcome.

Use FlowLens only on traffic and systems you own or are authorized to test.
