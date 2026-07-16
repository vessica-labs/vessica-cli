# Security

Document trust boundaries, privileged operations, authentication and authorization, secret sources, data sensitivity, sandboxing, redaction, dependency policy, and vulnerability reporting.

Treat network input and agent-generated output as untrusted. Never pass infrastructure secrets into an agent subprocess by inheriting the parent environment; use an explicit allowlist. Never log or persist credentials. Run untrusted commands with the least filesystem, user, network, and process privileges practical.

Single-tenant software still requires authentication, input validation, CSRF/origin controls where relevant, webhook verification, body limits, and secret isolation.
