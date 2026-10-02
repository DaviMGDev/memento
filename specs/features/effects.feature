Feature: Revertible effects

  Every context mutation is tracked with an inverse, and unloading a component
  runs its inverses in last-in-first-out order.

  Rule: A mutation registers the inverse that reverts it

    Scenario: A binding is reverted on unload
      Given a component that binds the key "storage" to a value
      When the component is unloaded
      Then the key "storage" is unbound
      And the context state is equivalent to its state before the component loaded

    Scenario: Several effects revert in reverse order
      Given a component that binds the key "storage"
      And the component then binds the key "cache"
      When the component is unloaded
      Then the inverse of "cache" runs before the inverse of "storage"

    Scenario: Unload is idempotent
      Given a component that binds the key "storage"
      When unload is requested twice
      Then the runtime reverts the binding exactly once

  Rule: A failed activation rolls back what it installed

    Scenario: Activation fails midway
      Given a component whose second effect fails
      When the component is activated
      Then the component state is "failed"
      And the first effect is reverted
      And the component is not activated again automatically

    Scenario: A panic during activation is contained
      Given a component whose activation panics
      When the component is activated
      Then the component state is "failed"
      And the effects installed before the panic are reverted

  Rule: Recovery is bounded by what the runtime can reify

    Scenario: An external emission survives unload
      Given a component that sends a message to an external peer
      When the component is unloaded
      Then the tracked binding is unbound
      But the peer keeps the message

    Scenario: A key with declared equivalence recovers up to it
      Given a key whose comparator treats two handles as equivalent
      And a component that renames such a handle
      When the component is unloaded
      Then the handle state is equivalent under the comparator
