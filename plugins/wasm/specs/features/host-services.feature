Feature: WASM host services — jobs, publish, and cancellation

  Guests reach host-owned jobs and events through imports routed to an
  injected HostServices contract. Documents are opaque bytes, results are
  stashed, and cancellation is a poll and a code.

  Rule: Job calls are stashed documents

    Scenario: A guest starts a job and reads the result back
      Given a guest whose activation starts a job with a request document
      When the host service returns a result document
      Then the call succeeds
      And the guest reads the document back in a second read

    Scenario: A job call reaches the host service with the caller
      Given a guest whose activation starts, peeps, and kills a job
      Then the host service receives each request document
      And the host service receives the calling instance

    Scenario: Job calls decline without host services
      Given an engine with no host services configured
      When a guest starts a job
      Then the import fails with the failure code

    Scenario: Job calls fail from a declaration probe
      Given a guest whose declaration starts a job
      Then the import fails with the failure code
      And the host service is never reached

  Rule: Publishing reaches the host bus

    Scenario: A guest publishes an event
      Given a guest whose activation publishes a topic and payload
      Then the host service receives the event

    Scenario: A service failure fails the publish
      Given a host service whose bus refuses the event
      When a guest publishes
      Then the import fails with the failure code

  Rule: Cancellation is a poll and a code

    Scenario: cancel_poll answers for the calling job only
      Given a guest whose activation polls for cancellation
      Then the host service answers for the calling instance
      And the guest observes not-canceled while the job lives

    Scenario: A killed caller's action imports fail canceled
      Given the calling job is killed
      When the guest starts a job
      Then the import fails with the canceled code
      And the job service is never reached

    Scenario: A killed caller's HTTP import fails before the exchange
      Given the calling job is killed
      When the guest performs an HTTP exchange
      Then the import fails with the canceled code
      And no exchange leaves the process

  Rule: The surface is additive

    Scenario: An engine without host services stays usable
      Given an engine with no host services configured
      Then existing imports keep their signatures and behavior
      And cancel_poll answers not-canceled
