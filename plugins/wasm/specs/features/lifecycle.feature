Feature: WASM component lifecycle

  The loader instantiates one module per activation, initializes WASI
  reactors, and reverts every guest effect on unload.

  Rule: One module instance per activation

    Scenario: Activation instantiates the module
      Given a compiled guest module is registered
      When its entry is loaded
      Then a fresh module instance runs the activation body

    Scenario: Reload reverts and reinstantiates
      Given a guest is active with registered effects
      When its entry payload changes
      Then its effects run in LIFO order
      And a fresh instance activates against the new payload

    Scenario: Unload closes the module after guest inverses
      Given a guest is active
      When the entry is removed
      Then the guest's inverses run before the module closes
      And no fiber or module remains

  Rule: Reactor initialization

    Scenario: A Go reactor is initialized before exports run
      Given a guest exports _rt0_wasm_wasip1_lib
      When the component activates
      Then the reactor runs before memento_activate

    Scenario: A core wasm guest is untouched
      Given a guest exports neither _initialize nor _rt0_wasm_wasip1_lib
      When the component activates
      Then instantiation proceeds without a reactor call
