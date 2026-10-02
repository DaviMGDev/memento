Feature: Declarative configuration

  A configuration is a tree of entries describing the desired composition; the
  loader reconciles it against the live fibers incrementally.

  Rule: Entries determine which fibers exist

    Scenario: Applying a configuration activates its entries
      Given a configuration with entries "database" and "console"
      When the configuration is applied
      Then both entries have active fibers

    Scenario: Disabling an entry unloads it
      Given a configuration with an active entry "database"
      When the entry is disabled
      Then the entry's fiber is unloaded
      And the rest of the configuration is untouched

    Scenario: Applying the same configuration twice changes nothing
      Given a configuration is applied
      When the same configuration is applied again
      Then no fiber transitions

  Rule: Reconciliation is incremental

    Scenario: A payload change reloads only its entry
      Given a configuration with active entries "database" and "console"
      When the payload of "database" changes
      Then only "database" reloads

    Scenario: A component change rebuilds its entry
      Given a configuration with an active entry "database"
      When the entry references a different component
      Then the old fiber is unloaded
      And a fiber of the new component is instantiated

    Scenario: A removed entry is unloaded
      Given a configuration with an active entry "console"
      When the entry is removed
      Then its fiber is unloaded

  Rule: Reconciliation converges to the from-scratch state

    Scenario: Incremental and direct application agree
      Given a configuration is applied
      And then revised several times
      When reconciliation reaches quiescence
      Then the resulting state is equivalent to a from-scratch load of the final configuration

    Scenario: Reconciliation never half-applies
      Given an entry whose component fails to activate during reconciliation
      When reconciliation runs
      Then the failing entry is "failed"
      And every other entry reaches its target state
