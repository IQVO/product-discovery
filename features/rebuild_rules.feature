# rebuild_rules.feature — documentation-derived scenarios for RebuildProjections (use-cases.md
# "RebuildProjections", ADR 0002 A6 / A12 / A16, ADR 0003, troubleshooting "Rebuild job"):
# the snapshot folds through the event rules, a failing page aborts and a re-run repairs.
Feature: The bootstrap rebuild folds the upstream snapshots through the event rules

  Scenario: A snapshot never rewinds a listing state an event already set
    Given the item master creates "RB-1" titled "Rewound Kettle"
    And offer-availability creates listing "LRB-1" for "RB-1" at node "N1"
    And offer-availability suppresses listing "LRB-1"
    And the snapshot store lists the live listing "LRB-1" of "RB-1" with 5 units at minute 3
    When the operator runs the rebuild
    Then the rebuild succeeds
    And the product "RB-1" is not in stock
    When offer-availability restores listing "LRB-1"
    Then the product "RB-1" is reported unpurchasable and in stock

  Scenario: A snapshot listing takes its suppressed state when no event set it
    Given the snapshot store lists the item "RB-2" titled "Suppressed Snapshot Kettle"
    And the snapshot store lists the suppressed listing "LRB-2" of "RB-2" with 5 units at minute 3
    When the operator runs the rebuild
    Then the rebuild succeeds
    And the product "RB-2" is not in stock

  Scenario: A discontinued catalogue row seeds a discontinued item
    Given the snapshot store lists the item "RB-3" titled "Gone Kettle" as discontinued
    When the operator runs the rebuild
    Then the rebuild folded 1 item, 0 listings, 0 prices, 0 contents and 0 categories
    And the product "RB-3" is not purchasable
    And the search for "gone" finds nothing

  Scenario: A draft content is counted but never listed
    Given the snapshot store lists the item "RB-4" titled "Draft Kettle"
    And the snapshot store lists the DRAFT content of "RB-4" at revision 1 in category "kettles"
    When the operator runs the rebuild
    Then the rebuild folded 1 item, 0 listings, 0 prices, 1 content and 0 categories
    And the search for "draft" finds nothing

  Scenario: A retired content row seeds a delisted product
    Given the snapshot store lists the item "RB-5" titled "Retired Kettle"
    And the snapshot store lists the RETIRED content of "RB-5" at revision 4 in category "kettles"
    When the operator runs the rebuild
    Then the search for "retired" finds nothing

  Scenario: A withdrawn price row is folded as a withdrawal and seeds the version guard
    Given the snapshot store lists the item "RB-6" titled "Unpriced Snapshot Kettle"
    And the snapshot store lists the withdrawn price of "RB-6" at version 4
    When the operator runs the rebuild
    Then the rebuild skipped 0 rows
    And the product "RB-6" shows no price
    When pricing sets version 3 of the price of "RB-6" to 500 with list price 500
    Then the product "RB-6" shows no price

  Scenario: An unpublished category tree is skipped
    Given the snapshot store holds the category tree at version 0 with:
      | home | | Home |
    When the operator runs the rebuild
    Then the rebuild succeeds
    And the rebuild folded 0 items, 0 listings, 0 prices, 0 contents and 0 categories
    And the category list is empty at version 0

  Scenario: A row that cannot be folded is skipped and counted while the others are folded
    Given the snapshot store lists the item "RB-7" titled "Good Kettle"
    And the snapshot store lists the price of "RB-7" at version 0 with effective 100 and list 100
    And the snapshot store lists the price of "RB-7" at version 2 with effective 2500 and list 2500
    When the operator runs the rebuild
    Then the rebuild succeeds
    And the rebuild skipped 1 row
    And the product "RB-7" has effective price 2500

  Scenario: The snapshot sources are read page by page
    Given the snapshot store lists the item "RB-8A" titled "First Kettle"
    And the snapshot store lists the item "RB-8B" titled "Second Kettle"
    And the snapshot store lists the item "RB-8C" titled "Third Kettle"
    And the snapshot sources serve 2 rows per page
    When the operator runs the rebuild
    Then the "catalog" source was read in 2 pages
    And the rebuild folded 3 items, 0 listings, 0 prices, 0 contents and 0 categories

  Scenario: A failing source page aborts the run at its step and a re-run repairs it
    Given the snapshot store lists the item "RB-9" titled "Aborted Kettle"
    And the snapshot store lists the live listing "LRB-9" of "RB-9" with 5 units at minute 3
    And the snapshot source "listings" is down
    When the operator runs the rebuild
    Then the rebuild stops at the step "listings"
    And the product "RB-9" is not in stock
    When the snapshot source "listings" is back
    And the operator runs the rebuild
    Then the rebuild succeeds
    And the product "RB-9" is reported unpurchasable and in stock

  Scenario: Events replayed from before the snapshot are dropped by the guards the snapshot seeded
    Given the snapshot store lists the item "RB-10" titled "Seeded Kettle"
    And the snapshot store lists the price of "RB-10" at version 5 with effective 4000 and list 4000
    And the snapshot store lists the live listing "LRB-10" of "RB-10" with 5 units at minute 20
    When the operator runs the rebuild
    And pricing sets version 4 of the price of "RB-10" to 1000 with list price 1000
    And offer-availability reports 0 units for listing "LRB-10" of "RB-10" at minute 19
    Then the product "RB-10" has effective price 4000
    And the product "RB-10" is reported unpurchasable and in stock

  Scenario: Running the rebuild twice leaves the same projection
    Given the snapshot store lists the item "RB-11" titled "Twice Kettle"
    And the snapshot store lists the price of "RB-11" at version 2 with effective 2500 and list 2500
    When the operator runs the rebuild
    And the shopper reads "/products/RB-11" noting its ETag
    And the operator runs the rebuild
    And the shopper reads "/products/RB-11" presenting the noted ETag
    Then the answer status is 304
