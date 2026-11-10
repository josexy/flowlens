# Spec Delta

## ADDED Requirements

### Requirement: Descriptive certificate and interpreter pickers
File pickers opened from Settings SHALL show the localized title and, where the platform shows one, a message naming the expected file. This covers the MITM CA certificate and key, upstream Root CA files, the client certificate and key, and the Python interpreter. Key pickers SHALL offer a filter named "Private Key Files".

#### Scenario: Choosing a client key on macOS
- **WHEN** the user browses for a client certificate's private key on macOS
- **THEN** the dialog explains that it expects the key matching the client certificate, and it offers the Private Key Files filter
