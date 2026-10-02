Feature: Component lifecycle

  A fiber is one instance of a component; its transitions are serialized and
  inertial, and failures are terminal until a revision re-inserts it.

  Rule: A fiber settles at the state its target calls for

    Scenario: Activation completes
      Given a component whose declared keys are satisfied
      When the component is loaded
      Then the component passes through "loading"
      And the component settles at "active"

    Scenario: Unloading completes
      Given an active component
      When the component is retired
      Then the component passes through "unloading"
      And the component settles at "inactive"

    Scenario: Deactivation chained from a target change
      Given an active component
      When its dependency is withdrawn while one of its effects is still installing
      Then the current activation completes
      And the component then unloads
      And the component settles at "inactive"

  Rule: A transition in flight is inert

    Scenario: A target change during activation is honored after it completes
      Given a component is "loading"
      When its dependency is withdrawn before activation finishes
      Then the activation runs to completion
      And the component deactivates without a further trigger

    Scenario: A target change during unload is honored after it completes
      Given a component is "unloading"
      When its dependency is provided again before unload finishes
      Then the unload runs to completion
      And the component activates again

  Rule: Failures are terminal until a revision

    Scenario: A failed component stays failed
      Given a component whose activation fails
      When the same component is reloaded against the same environment
      Then the component state remains "failed"

    Scenario: Re-enabling a failed entry starts a fresh instance
      Given a failed component is disabled
      When the entry is enabled again
      Then a new fiber is instantiated
      And the failure is not carried over

  Rule: Instances are independent

    Scenario: Unloading one instance leaves its siblings active
      Given two active components that declare the same key
      When one is unloaded
      Then the other stays "active"
      And the shared provider is unaffected
