Feature: In-process Lua plugins

  Hosts may load trusted Lua components from a configured directory. A script
  can access only host-registered typed keys declared in its module.

  Scenario: A Lua provider participates in dependency lifecycle
    Given a Lua plugin "provider.lua" providing key "message"
    When the Lua composition is applied with payload "hello from Lua"
    Then the reader observes "hello from Lua"
    When the Lua provider is disabled
    Then the Lua provider is unloaded and its reader deactivates
