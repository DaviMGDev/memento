Feature: WASM host HTTP transport

  The loader performs HTTP exchanges on a guest's behalf under a host-owned
  egress policy. The guest writes a JSON request document; the host performs
  the request and stashes a JSON response document the guest reads back.

  Rule: The host performs the exchange

    Scenario: A guest performs a request
      Given a guest that imports the HTTP transport
      And the host transport answers with a response document
      When the guest calls http_request
      Then it returns success
      And the guest reads the response with http_response_len and http_response

    Scenario: A malformed request document fails without an exchange
      Given a guest whose request document is not valid JSON
      When the guest calls http_request
      Then it returns non-zero
      And the host transport is never called

  Rule: The host owns egress policy

    Scenario: An allow-list admits a listed host
      Given the engine allows only "allowed.test"
      When a guest requests "https://allowed.test/ping"
      Then the host transport performs the exchange

    Scenario: An allow-list refuses an unlisted host
      Given the engine allows only "allowed.test"
      When a guest requests "https://blocked.test/ping"
      Then http_request returns non-zero
      And the host transport is never called

    Scenario: A request that outlives the timeout fails
      Given the engine bounds exchanges at a short timeout
      And the host transport holds the exchange open
      When the guest calls http_request
      Then it returns non-zero

  Rule: Credentials are host-substituted references

    Scenario: A reference is replaced by the host secret
      Given the engine resolves "env:TEST_KEY" to a secret
      When a guest sends the header "Bearer env:TEST_KEY"
      Then the transport receives the resolved secret

    Scenario: An unavailable reference fails the exchange
      Given the engine resolves no such reference
      When a guest sends the header "Bearer env:MISSING"
      Then http_request returns non-zero
      And the host transport is never called

  Rule: Declarations are pure

    Scenario: A request during declaration fails
      Given a guest calls http_request from memento_declare
      When the component is compiled and inspected
      Then declaration fails
      And the host transport is never called
