# folding_wave2.feature — wave 2: the removal facts (a retired content and a
# withdrawn price delist the product), listing creation activating an item,
# and the bootstrap rebuild folding the upstream snapshots through the same
# rules the events use.
Feature: The removal facts and the bootstrap rebuild

  Background:
    Given the category tree "Home > Kettles" is published

  Scenario: A retired content delists the product
    Given a purchasable product "K1" titled "Steel Kettle" priced 3000 in category "kettles"
    When the shopper searches for "kettle"
    Then the hits are "K1" in any order
    When product-content retires "K1"
    Then the search for "kettle" finds nothing
    And the product "K1" is not purchasable

  Scenario: A withdrawn price keeps the product browsable but unpurchasable
    Given a purchasable product "K1" titled "Steel Kettle" priced 3000 in category "kettles"
    When pricing withdraws the price of "K1"
    And the shopper searches for "kettle"
    Then the hits are "K1" in any order
    And the product "K1" is not purchasable

  Scenario: A created listing activates the stocked item
    Given the item master creates "K1" titled "Steel Kettle"
    And product-content publishes revision 1 for "K1" in category "kettles"
    And pricing sets the price of "K1" to 3000 with list price 3000
    When the shopper searches for "kettle"
    Then the hits are "K1" in any order
    And the product "K1" is not in stock
    When offer-availability creates listing "L-K1" for "K1" at node "N1"
    And offer-availability lists "L-K1" for "K1" with 5 units
    And the shopper searches for "kettle"
    Then the hits are "K1" in any order
    And the product "K1" is purchasable and in stock

  Scenario: The bootstrap rebuild folds the snapshots through the event rules
    Given the snapshot store holds one purchasable product "K1" titled "Steel Kettle" priced 3000 in category "kettles"
    And the snapshot store holds the category tree "Home > Kettles"
    When the projection rebuild runs over the upstream snapshots
    Then the rebuild folded 1 item, 1 listing, 1 price, 1 content and 2 categories
    When the shopper searches for "kettle"
    Then the hits are "K1" in any order
    And the product "K1" has the category path "Home > Kettles"

  Scenario: A bad cursor is a validation failure and the platform endpoints answer
    Given a purchasable product "K1" titled "Steel Kettle" priced 3000 in category "kettles"
    When the shopper asks for the page after "not-a-cursor"
    Then the answer is 400 "invalid-cursor"
    When the platform endpoint "/healthz" is requested
    Then the answer status is 200
    When the platform endpoint "/readyz" is requested
    Then the answer status is 200
