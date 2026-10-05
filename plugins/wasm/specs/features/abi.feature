Feature: WASM host ABI — registration, reads, and invocation

  The loader exposes the kernel's set/get and value operations to guests.
  Registration is a tracked effect, dependents read resolved values, and a
  dependent may invoke the operations of the value bound at a key.

  Rule: A provide is registered by binding

    Scenario: A guest binds its declared provide
      Given a guest that declares it provides "cache"
      When its activation binds "cache" to a value
      Then the key "cache" is provided by the guest's fiber
      And a dependent of "cache" may activate

    Scenario: A declared provide left unbound fails activation
      Given a guest that declares it provides "cache"
      When its activation returns without binding "cache"
      Then activation fails with a descriptive error

    Scenario: Unload reverts the registration
      Given a guest has bound "cache" and is active
      When the guest unloads
      Then the binding is withdrawn
      And dependents of "cache" deactivate first

    Scenario: A replacement provider registers afresh
      Given a guest provides "cache" with one value
      When a replacement provider binds "cache"
      Then dependents observe the new provider identity
      And the old registration is withdrawn with the old module

  Rule: Dependents read values

    Scenario: get reads a bound value
      Given a guest declares it injects "cache" and is active
      When it reads "cache"
      Then it receives the bytes bound by the provider

    Scenario: get resolves through the committed view during teardown
      Given a guest that injects "cache" is unloading
      When the provider withdraws
      Then reading "cache" still resolves the committed provider value

  Rule: Invocation routes to the provider

    Scenario: A dependent invokes an operation
      Given a guest declares it injects "cache" and is active
      And the provider of "cache" exports memento_alloc and memento_handle
      When the guest invokes "cache" with a request
      Then the response is the provider's handler result

    Scenario: A provider without a handler yields no response
      Given a guest declares it injects "cache" and is active
      And the provider exports no memento_handle
      When the guest invokes "cache"
      Then the invocation returns no bytes

    Scenario: A non-dependent cannot invoke
      Given a component that does not declare "cache" injected
      When it invokes "cache"
      Then the invocation fails
