Feature: Folding catalogue, content and price facts into one product view
  The read side builds one ProductView per catalogue item from facts that
  arrive in any order. An item is purchasable only when the item master, the
  content, the price and the stock all agree.

  Scenario: Eleven kinds of fact make one purchasable product
    Given the category tree "Home > Kettles" is published
    When the item master creates "I1" titled "Steel Kettle"
    And offer-availability lists "L1" for "I1" with 5 units
    And product-content publishes revision 1 for "I1" in category "kettles"
    And pricing sets the price of "I1" to 9990 with list price 12990
    Then the product "I1" is purchasable and in stock
    And the product "I1" has the category path "Home > Kettles"

  Scenario: Facts that arrive before the item master are invisible until it catches up
    When pricing sets the price of "I2" to 500 with list price 500
    And product-content publishes revision 1 for "I2" in category "kettles"
    Then the product "I2" is not found
    When the item master creates "I2" titled "Late Kettle"
    Then the product "I2" has effective price 500

  Scenario: A discontinued item is never purchasable
    Given a purchasable product "I3" titled "Doomed Kettle"
    When the item master discontinues "I3"
    Then the product "I3" is not purchasable
    And the search for "doomed" finds nothing

  Scenario: A suppressed listing takes the item out of stock
    Given a purchasable product "I4" titled "Seasonal Kettle"
    When offer-availability suppresses listing "L-I4"
    Then the product "I4" is not in stock
    And the product "I4" is not purchasable
    When offer-availability restores listing "L-I4"
    Then the product "I4" is purchasable and in stock

  Scenario: An older availability is dropped by the per-listing guard
    Given a purchasable product "I5" titled "Guarded Kettle"
    When offer-availability reports 0 units for "L-I5" as of an older instant
    Then the product "I5" is purchasable and in stock

  Scenario: A redelivered event is applied once
    When the item master creates "I6" titled "Once" twice with the same event id
    Then the product "I6" has the title "Once"
