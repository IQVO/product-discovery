# fold_rules.feature — documentation-derived scenarios (docs/docs/ddd/use-cases.md "Folds",
# ADR 0002 A1-A18, the troubleshooting table): the guards, the validity limits and the
# redelivery rules of the projector, one rule per scenario.
Feature: The rules every consumed fact is folded under

  Scenario Outline: A catalogue title is accepted up to 500 characters and refused beyond
    When the item master announces "I-BOUNDARY" titled with <length> characters
    Then the fact is <outcome>

    Examples:
      | length | outcome            |
      | 500    | accepted           |
      | 501    | refused as invalid |

  Scenario: A blank title is refused and leaves no visible row
    When the item master announces the item "I-BLANK" titled ""
    Then the fact is refused as invalid
    And the product "I-BLANK" is not found

  Scenario: A title is trimmed before it is stored
    When the item master announces the item "I-TRIM" titled "  Steel Kettle  "
    Then the product "I-TRIM" has the title "Steel Kettle"

  Scenario: The title of an announced item is immutable
    Given the item master creates "I-IMM" titled "Steel Kettle"
    When the item master announces the item "I-IMM" titled "Renamed Kettle"
    Then the fact is accepted
    And the product "I-IMM" has the title "Steel Kettle"

  Scenario: The hazmat and fragile flags of the item master reach the product
    When the item master creates "I-FUEL" titled "Fuel Canister" with hazmat true and fragile false
    And the shopper opens the product "I-FUEL"
    Then the response field "hazmat" is true
    And the response field "fragile" is false

  Scenario Outline: An item id is accepted up to 64 characters and refused beyond
    When the item master announces an item whose id is <length> characters long
    Then the fact is <outcome>

    Examples:
      | length | outcome            |
      | 64     | accepted           |
      | 65     | refused as invalid |

  Scenario: A discontinue that arrives before the create cannot be revived by the late create
    Given the item master discontinues "I-D1"
    And a purchasable product "I-D1" titled "Late Kettle"
    Then the product "I-D1" has the title "Late Kettle"
    And the product "I-D1" is not purchasable
    And the search for "late" finds nothing

  Scenario Outline: A listing availability applies only when its asOf is strictly newer
    Given the item master creates "G-1" titled "Guard Kettle"
    And offer-availability reports 5 units for listing "LG-1" of "G-1" at minute 10
    When offer-availability reports 0 units for listing "LG-1" of "G-1" at minute <minute>
    Then the product "G-1" is reported unpurchasable and <stock>

    Examples:
      | minute | stock        |
      | 9      | in stock     |
      | 10     | in stock     |
      | 11     | out of stock |

  Scenario: A redelivered availability event is dropped by the inbox even with a newer asOf
    Given the item master creates "R-1" titled "Redelivered Kettle"
    When offer-availability delivers 5 units for listing "LR-1" of "R-1" at minute 1 and then the same event again with 0 units at minute 2
    Then the product "R-1" is reported unpurchasable and in stock

  Scenario: A redelivered price event is dropped by the inbox even with a higher version
    Given the item master creates "R-2" titled "Redelivered Price Kettle"
    When pricing delivers version 1 of the price of "R-2" at 3000 and then the same event again claiming version 2 at 9000
    Then the product "R-2" has effective price 3000

  Scenario: In stock needs one live listing with a quantity above zero
    Given the item master creates "M-1" titled "Multi Listing Kettle"
    When offer-availability reports 0 units for listing "LM-1" of "M-1" at minute 1
    Then the product "M-1" is not in stock
    When offer-availability reports 3 units for listing "LM-2" of "M-1" at minute 1
    Then the product "M-1" is reported unpurchasable and in stock
    When offer-availability suppresses listing "LM-2"
    Then the product "M-1" is not in stock
    When offer-availability restores listing "LM-2"
    Then the product "M-1" is reported unpurchasable and in stock

  Scenario: A suppressed listing keeps folding its quantity and restores with the latest one
    Given the item master creates "M-2" titled "Suppressed Quantity Kettle"
    And offer-availability reports 2 units for listing "LS-1" of "M-2" at minute 1
    And offer-availability suppresses listing "LS-1"
    When offer-availability reports 0 units for listing "LS-1" of "M-2" at minute 2
    And offer-availability restores listing "LS-1"
    Then the product "M-2" is not in stock

  Scenario: A suppress for a listing never learned is dropped, not queued
    Given the item master creates "U-1" titled "Ghost Listing Kettle"
    When offer-availability suppresses listing "L-GHOST"
    And offer-availability creates listing "L-GHOST" for "U-1" at node "N1"
    And offer-availability lists "L-GHOST" for "U-1" with 4 units
    Then the product "U-1" is reported unpurchasable and in stock
    When offer-availability suppresses listing "L-GHOST"
    Then the product "U-1" is not in stock

  Scenario Outline: A listing quantity is accepted from zero and refused below
    When offer-availability announces listing "LN-1" of "N-1" with <units> units
    Then the fact is <outcome>

    Examples:
      | units | outcome            |
      | 0     | accepted           |
      | -1    | refused as invalid |

  Scenario: A listing availability without an as-of instant is refused
    When offer-availability announces listing "LN-2" of "N-2" with 3 units and no as-of instant
    Then the fact is refused as invalid

  Scenario: A redelivered category tree is dropped by the inbox even with a higher version
    When the category tree is delivered at version 1 as "Kettles" and then the same event again at version 2 as "Boilers"
    Then the product catalogue still shows the category "kettles" named "Kettles"

  Scenario Outline: A price change applies only at a strictly higher version
    Given the item master creates "P-1" titled "Priced Kettle"
    And pricing sets version 5 of the price of "P-1" to 4000 with list price 5000
    When pricing sets version <version> of the price of "P-1" to 1000 with list price 1000
    Then the product "P-1" has effective price <effective>

    Examples:
      | version | effective |
      | 4       | 4000      |
      | 5       | 4000      |
      | 6       | 1000      |

  Scenario Outline: A withdrawal applies unless a newer price version was already folded
    Given the item master creates "P-2" titled "Withdrawn Kettle"
    And pricing sets version 3 of the price of "P-2" to 3000 with list price 3000
    When pricing withdraws the price of "P-2" at version <version>
    Then the product "P-2" shows <shown>

    Examples:
      | version | shown                      |
      | 2       | the effective price 3000   |
      | 3       | no price                   |
      | 4       | no price                   |

  Scenario: A replayed older price change cannot revive a withdrawn price but a newer one does
    Given the item master creates "P-3" titled "Revived Kettle"
    And pricing sets version 1 of the price of "P-3" to 1500 with list price 1500
    And pricing withdraws the price of "P-3" at version 3
    When pricing sets version 2 of the price of "P-3" to 900 with list price 900
    Then the product "P-3" shows no price
    When pricing sets version 4 of the price of "P-3" to 2000 with list price 2000
    Then the product "P-3" shows the effective price 2000

  Scenario: A withdrawn price keeps no amounts on the product page
    Given the item master creates "P-4" titled "Cleared Kettle"
    And pricing sets version 1 of the price of "P-4" to 1500 with list price 1800
    And pricing withdraws the price of "P-4" at version 2
    When the shopper opens the product "P-4"
    Then the response has no field "listPrice"
    And the response has no field "effectivePrice"

  Scenario: The list price and the effective price are stored apart with their currency
    Given the item master creates "P-5" titled "Sale Kettle"
    When pricing announces version 1 of the price of "P-5" in currency "EUR" at effective 9990 and list 12990
    And the shopper opens the product "P-5"
    Then the response field "listPrice.amountMinor" is 12990
    And the response field "effectivePrice.amountMinor" is 9990
    And the response field "effectivePrice.currency" is "EUR"

  Scenario Outline: A price fact needs a version from 1, a three-letter capital currency and no negative amount
    When pricing announces version <version> of the price of "P-6" in currency "<currency>" at effective <effective> and list <list>
    Then the fact is <outcome>

    Examples:
      | version | currency | effective | list | outcome            |
      | 1       | USD      | 0         | 0    | accepted           |
      | 0       | USD      | 100       | 100  | refused as invalid |
      | 1       | usd      | 100       | 100  | refused as invalid |
      | 1       | US       | 100       | 100  | refused as invalid |
      | 1       | USD      | -1        | 100  | refused as invalid |
      | 1       | USD      | 100       | -1   | refused as invalid |

  Scenario Outline: A content publication applies only at a strictly higher revision
    Given the item master creates "C-1" titled "Copy Kettle"
    And a content draft described "Original copy"
    And product-content publishes the draft as revision 3 for "C-1" in category "kettles"
    And a content draft described "Replacement copy"
    When product-content publishes the draft as revision <revision> for "C-1" in category "kettles"
    And the shopper opens the product "C-1"
    Then the response field "description" is "<description>"

    Examples:
      | revision | description      |
      | 2        | Original copy    |
      | 3        | Original copy    |
      | 4        | Replacement copy |

  Scenario Outline: A content revision starts at 1
    When product-content announces revision <revision> for "C-2" in category "kettles"
    Then the fact is <outcome>

    Examples:
      | revision | outcome            |
      | 1        | accepted           |
      | 0        | refused as invalid |
      | -1       | refused as invalid |

  Scenario Outline: A retire applies unless it names an older revision than the one folded
    Given a purchasable product "C-3" titled "Retire Kettle"
    And product-content publishes revision 3 for "C-3" in category "kettles"
    When product-content retires "C-3" at revision <revision>
    Then the product "C-3" is reported <verdict> and in stock

    Examples:
      | revision | verdict       |
      | 2        | purchasable   |
      | 3        | unpurchasable |
      | 4        | unpurchasable |
      | 0        | unpurchasable |

  Scenario: A retired content is revived only by a strictly higher revision
    Given a purchasable product "C-4" titled "Revive Kettle"
    And product-content retires "C-4" at revision 1
    When product-content publishes revision 1 for "C-4" in category "kettles"
    Then the product "C-4" is reported unpurchasable and in stock
    When product-content publishes revision 2 for "C-4" in category "kettles"
    Then the product "C-4" is reported purchasable and in stock

  Scenario: A retired content hides the description and the category path but not the product
    Given the category tree "Home > Kettles" is published
    And a purchasable product "C-5" titled "Hidden Copy Kettle" priced 3000 in category "kettles"
    When product-content retires "C-5"
    And the shopper opens the product "C-5"
    Then the response field "title" is "Hidden Copy Kettle"
    And the response has no field "description"
    And the response has no field "categoryPath"

  Scenario: Bullets, attributes with their unit and media reach the product page
    Given the item master creates "C-6" titled "Aquila Travel Kettle"
    And a content draft described "A one litre travel kettle"
    And the draft has the bullet "Boils in 3 minutes"
    And the draft has the bullet "Folds flat"
    And the draft has the attribute "Capacity" = "1" in "L"
    And the draft has the attribute "Colour" = "Teal"
    And the draft has the PRIMARY image "img-1"
    And the draft has the GALLERY image "img-2"
    And product-content publishes the draft as revision 1 for "C-6" in category "kettles"
    When the shopper opens the product "C-6"
    Then the response field "description" is "A one litre travel kettle"
    And the response list "bullets" has 2 items
    And the response field "bullets.1" is "Folds flat"
    And the response field "attributes.0.unit" is "L"
    And the response has no field "attributes.1.unit"
    And the response list "media" has 2 items
    And the response field "media.0.role" is "PRIMARY"

  Scenario: A newer content revision replaces the content wholesale
    Given the item master creates "C-7" titled "Replaced Kettle"
    And a content draft described "First edition"
    And the draft has the bullet "Old bullet"
    And product-content publishes the draft as revision 1 for "C-7" in category "kettles"
    And a content draft described "Second edition"
    And product-content publishes the draft as revision 2 for "C-7" in category "kettles"
    When the shopper opens the product "C-7"
    Then the response field "description" is "Second edition"
    And the response has no field "bullets"
