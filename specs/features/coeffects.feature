Feature: Reactive coeffects

  A component declares the keys it requires; the runtime activates it when they
  are satisfied and deactivates it when they are lost.

  Rule: Activation waits for satisfaction

    Scenario: A missing dependency keeps the component inactive
      Given a component that declares the key "storage"
      And no provider of "storage" is active
      When the component is loaded
      Then the component state is "inactive"

    Scenario: A dependency appears
      Given a component that declares the key "storage" is loaded and inactive
      When a provider of "storage" activates
      Then the component activates

    Scenario: A dependency is withdrawn
      Given a component that declares the key "storage" is active
      When the provider of "storage" starts unloading
      Then the component deactivates before the provider runs its inverses

    Scenario: Teardown can read the withdrawing dependency
      Given a component that declares the key "storage" is active
      When the provider of "storage" starts unloading
      Then the component reads "storage" during its own teardown
      And the provider runs its inverses only after the component is inactive

  Rule: Change classification uses provider identity and declarations

    Scenario: An in-place overwrite is neutral
      Given a component that declares the key "storage" is active
      When its provider overwrites the bound value in place
      Then the component stays active

    Scenario: A replaced provider reactivates the component
      Given a component that declares the key "storage" is active
      When a different provider provides "storage"
      Then the component deactivates
      And the component activates against the new provider

    Scenario: An undeclared key is not observed
      Given a component that declares the key "storage" is active
      When a provider of "cache" activates
      Then the component makes no transition

  Rule: Dependencies are acyclic

    Scenario: A cycle is refused at insertion
      Given a component that provides "alpha" and declares "beta"
      And a component that provides "beta" and declares "alpha"
      When the second component is inserted
      Then the insertion fails with a dependency cycle error
      And the registry is unchanged
