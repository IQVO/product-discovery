# read_rules.feature — documentation-derived scenarios for the read side (use-cases.md
# SearchProducts / GetProduct / ListCategories, ADR 0002 A2-A9 and A14-A15, the
# troubleshooting table "Why is this product wrong?"): what is listed, filtered, ranked,
# counted and suggested, one rule per scenario.
Feature: What the shopper sees: visibility, search rules, facets and the category view

  Scenario: A listed product without stock is shown as currently unavailable, not hidden
    Given the item master creates "V-1" titled "Slate Kettle"
    And product-content publishes revision 1 for "V-1" in category "kettles"
    And pricing sets the price of "V-1" to 3000 with list price 3000
    When the shopper searches for "slate"
    Then the hit "V-1" is unpurchasable and out of stock

  Scenario: A product whose content was never published is readable but not listed
    Given the item master creates "V-2" titled "Unpublished Kettle"
    And offer-availability lists "L-V2" for "V-2" with 4 units
    And pricing sets the price of "V-2" to 3000 with list price 3000
    Then the product "V-2" is reported unpurchasable and in stock
    And the search for "unpublished" finds nothing

  Scenario: Price filters and price sorts list only priced rows, the other sorts list unpriced ones too
    Given a purchasable product "PR-1" titled "Priced Pot" priced 3000 in category "kettles"
    And the item master creates "PR-2" titled "Unpriced Pot"
    And product-content publishes revision 1 for "PR-2" in category "kettles"
    When the shopper searches with sort "title"
    Then the hits are exactly "PR-1, PR-2"
    And the hit "PR-2" shows no price
    When the shopper searches with sort "price-asc"
    Then the hits are exactly "PR-1"
    When the shopper searches with price from 0 to 99999 sorted by "title"
    Then the hits are exactly "PR-1"

  Scenario Outline: The price bounds include both edges
    Given a purchasable product "B-2000" titled "Pot Two Thousand" priced 2000 in category "kettles"
    And a purchasable product "B-3000" titled "Pot Three Thousand" priced 3000 in category "kettles"
    And a purchasable product "B-4000" titled "Pot Four Thousand" priced 4000 in category "kettles"
    When the shopper requests "/products?priceMin=<min>&priceMax=<max>&sort=price-asc"
    Then the hits are exactly "<hits>"

    Examples:
      | min  | max  | hits                       |
      | 3000 | 3000 | B-3000                     |
      | 2001 | 3999 | B-3000                     |
      | 2000 | 4000 | B-2000, B-3000, B-4000     |

  Scenario Outline: A filter out of range or of the wrong type is a validation failure
    When the shopper requests "/products?<query>"
    Then the answer is 422 "validation-failed"

    Examples:
      | query                      |
      | priceMin=4000&priceMax=3000 |
      | priceMin=-1                |
      | priceMax=-5                |
      | priceMin=ten               |
      | inStock=yes                |
      | inStock=TRUE               |

  Scenario Outline: The free text is accepted up to 200 characters
    When the shopper searches for a text of <length> characters
    Then the answer status is <status>

    Examples:
      | length | status |
      | 200    | 200    |
      | 201    | 422    |

  Scenario Outline: The in-stock filter lists the products in or out of stock
    Given a purchasable product "S-1" titled "Stocked Pan" priced 3000 in category "kettles"
    And the item master creates "S-2" titled "Empty Pan"
    And product-content publishes revision 1 for "S-2" in category "kettles"
    And pricing sets the price of "S-2" to 3000 with list price 3000
    When the shopper requests "/products?inStock=<flag>"
    Then the hits are exactly "<hits>"

    Examples:
      | flag  | hits |
      | true  | S-1  |
      | false | S-2  |

  Scenario: Title outranks attribute, which outranks description and bullets
    Given the item master creates "RK-1" titled "Aqua Kettle"
    And the item master creates "RK-2" titled "Brew Master"
    And the item master creates "RK-3" titled "Tea Maker"
    And the item master creates "RK-4" titled "Leaf Steeper"
    And a content draft described "A plain jug"
    And product-content publishes the draft as revision 1 for "RK-1" in category "kettles"
    And a content draft described "A plain jug"
    And the draft has the attribute "Material" = "kettle steel"
    And product-content publishes the draft as revision 1 for "RK-2" in category "kettles"
    And a content draft described "A kettle alternative"
    And product-content publishes the draft as revision 1 for "RK-3" in category "kettles"
    And a content draft described "A plain jug"
    And the draft has the bullet "Kettle safe"
    And product-content publishes the draft as revision 1 for "RK-4" in category "kettles"
    When the shopper searches for "kettle"
    Then the hits are exactly "RK-1, RK-2, RK-3, RK-4"

  Scenario: A category name matches the query without a copy in the product row
    Given the category tree "Home > Kettles" is published
    And a purchasable product "CN-1" titled "Aquila 2L" priced 4000 in category "kettles"
    And a purchasable product "CN-2" titled "Spare Lid" priced 900 in category "home"
    When the shopper searches for "kettles"
    Then the hits are exactly "CN-1"

  Scenario: The category filter covers every level below and no sibling
    Given the category tree is published at version 3 with:
      | home    |         | Home    |
      | kitchen | home    | Kitchen |
      | kettles | kitchen | Kettles |
      | garden  | home    | Garden  |
    And a purchasable product "CF-1" titled "Deep Kettle" priced 3000 in category "kettles"
    And a purchasable product "CF-2" titled "Kitchen Scale" priced 3000 in category "kitchen"
    And a purchasable product "CF-3" titled "Garden Hose" priced 3000 in category "garden"
    When the shopper searches inside category "kitchen" sorted by "title"
    Then the hits are exactly "CF-1, CF-2"
    When the shopper requests "/products?categoryId=nowhere"
    Then there are no hits

  Scenario: A product-type query suggests at most five categories, the shallowest first
    Given the category tree is published at version 3 with:
      | home    |         | Home          |
      | kitchen | home    | Kitchen       |
      | kettles | home    | Kettles       |
      | gk      | kitchen | Glass Kettles |
      | sk      | kitchen | Steel Kettles |
      | tk      | kitchen | Travel Kettles |
      | ks      | kitchen | Kettle Spares |
      | kf      | kitchen | Kettle Filters |
      | gf      | kitchen | Gift Kettles  |
    When the shopper searches for "kettles"
    Then there are 5 category suggestions
    And the first category suggestion is "kettles"

  Scenario: A category is suggested only when its name covers every word of the query
    Given the category tree is published at version 3 with:
      | home | | Home          |
      | sk   | home | Steel Kettles |
      | gk   | home | Glass Kettles |
    When the shopper searches for "steel kettles"
    Then the category suggestions are "sk"
    When the shopper searches for "red kettles"
    Then there are 0 category suggestions

  Scenario: A browse without text suggests no category
    Given the category tree "Home > Kettles" is published
    And a purchasable product "BR-1" titled "Browse Kettle" priced 3000 in category "kettles"
    When the shopper searches with sort "title"
    Then the response has no field "categorySuggestions"

  Scenario: The typo fallback does not widen a search that already has full-text hits
    Given a purchasable product "T-1" titled "Kettle Pro" priced 3000 in category "kettles"
    And a purchasable product "T-2" titled "Kettel Deluxe" priced 3000 in category "kettles"
    When the shopper searches for "kettle"
    Then the hits are exactly "T-1"

  Scenario: Later pages of a typo search stay in trigram mode
    Given a purchasable product "TG-1" titled "Steel Kettle" priced 3000 in category "kettles"
    And a purchasable product "TG-2" titled "Glass Kettle" priced 3000 in category "kettles"
    And a purchasable product "TG-3" titled "Copper Kettle" priced 3000 in category "kettles"
    When the shopper pages through "q=ketle" 1 at a time
    Then the pages concatenate to "TG-1, TG-2, TG-3" in order

  Scenario: A query far from every title finds nothing even through the fallback
    Given a purchasable product "TF-1" titled "Steel Kettle" priced 3000 in category "kettles"
    When the shopper searches for "zzqxv"
    Then there are no hits
    And there is no next page

  Scenario: The title sort ignores case
    Given a purchasable product "TS-1" titled "banana Pot" priced 3000 in category "kettles"
    And a purchasable product "TS-2" titled "Apple Pan" priced 3000 in category "kettles"
    And a purchasable product "TS-3" titled "cherry Wok" priced 3000 in category "kettles"
    When the shopper searches with sort "title"
    Then the hits are exactly "TS-2, TS-1, TS-3"

  Scenario: Equal prices are tied by catalogue item id so no page skips or repeats a row
    Given a purchasable product "PT-1" titled "Twin Pot One" priced 3000 in category "kettles"
    And a purchasable product "PT-2" titled "Twin Pot Two" priced 3000 in category "kettles"
    And a purchasable product "PT-3" titled "Dear Pot" priced 5000 in category "kettles"
    When the shopper pages through everything sorted by "price-desc" 1 at a time
    Then the pages concatenate to "PT-3, PT-1, PT-2" in order

  Scenario Outline: A page that exactly fills the limit has no next page
    Given a purchasable product "PG-1" titled "Page Pot One" priced 3000 in category "kettles"
    And a purchasable product "PG-2" titled "Page Pot Two" priced 3000 in category "kettles"
    And a purchasable product "PG-3" titled "Page Pot Three" priced 3000 in category "kettles"
    When the shopper searches with page size <limit>
    Then there is <next> next page

    Examples:
      | limit | next |
      | 3     | no   |
      | 2     | a    |

  Scenario Outline: A price lands in the band that includes its lower edge
    Given a purchasable product "BD-1" titled "Band Pot" priced <price> in category "kettles"
    When the shopper searches for "band"
    Then the facet "price-band" has "<band>" with count 1

    Examples:
      | price | band        |
      | 2499  | <2500       |
      | 2500  | 2500-4999   |
      | 4999  | 2500-4999   |
      | 5000  | 5000-9999   |
      | 10000 | 10000-24999 |
      | 24999 | 10000-24999 |
      | 25000 | 25000+      |

  Scenario Outline: The band edges are configurable and the labels follow them
    Given the price bands are cut at 1000 and 2000
    And a purchasable product "BC-1" titled "Custom Band Pot" priced <price> in category "kettles"
    When the shopper searches for "custom"
    Then the facet "price-band" has "<band>" with count 1

    Examples:
      | price | band      |
      | 999   | <1000     |
      | 1500  | 1000-1999 |
      | 2000  | 2000+     |

  Scenario: Facets are listed category, price band, in stock and drop the empty buckets
    Given a purchasable product "FC-1" titled "Facet Pan" priced 3000 in category "kettles"
    When the shopper searches for "facet"
    Then the facets are listed in the order "category, price-band, in-stock"
    And the facet "in-stock" has "true" with count 1
    And the facet "in-stock" has no "false" bucket

  Scenario: An unpriced row counts in no price band
    Given a purchasable product "FP-1" titled "Facet Priced Pan" priced 3000 in category "kettles"
    And the item master creates "FP-2" titled "Facet Unpriced Pan"
    And product-content publishes revision 1 for "FP-2" in category "kettles"
    When the shopper searches for "facet"
    Then the facet "price-band" has "2500-4999" with count 1
    And the facet "in-stock" has "false" with count 1

  Scenario: A purchasable count follows the verdict of the products
    Given the category tree "Home > Kettles" is published
    And a purchasable product "PC-1" titled "Counted Kettle" priced 3000 in category "kettles"
    Then the category "kettles" has 1 purchasable products
    When offer-availability reports 0 units for listing "L-PC-1" of "PC-1" at minute 5
    Then the category "kettles" has 0 purchasable products
    When offer-availability reports 3 units for listing "L-PC-1" of "PC-1" at minute 6
    Then the category "kettles" has 1 purchasable products
    When pricing withdraws the price of "PC-1"
    Then the category "kettles" has 0 purchasable products

  Scenario: A purchasable count rolls up every level of the tree and no sibling
    Given the category tree is published at version 3 with:
      | home    |         | Home    |
      | kitchen | home    | Kitchen |
      | kettles | kitchen | Kettles |
      | garden  | home    | Garden  |
    And a purchasable product "RC-1" titled "Rolled Kettle" priced 3000 in category "kettles"
    Then the category "kettles" has 1 purchasable products
    And the category "kitchen" has 1 purchasable products
    And the category "home" has 1 purchasable products
    And the category "garden" has 0 purchasable products

  Scenario Outline: A category tree replaces the current one only at a strictly higher version
    Given the category tree "Home > Kettles" is published
    When the category tree is republished as version <version> with "kettles" renamed "Boilers"
    Then the product catalogue still shows the category "kettles" named "<name>"

    Examples:
      | version | name    |
      | 4       | Kettles |
      | 5       | Kettles |
      | 6       | Boilers |

  Scenario Outline: A tree that cannot be a tree is refused and the current one stays
    Given the category tree "Home > Kettles" is published
    When the category tree is announced at version 9 with a <defect>
    Then the fact is refused as invalid
    And the product catalogue still shows the category "kettles" named "Kettles"

    Examples:
      | defect                         |
      | duplicate category id          |
      | parent that is not in the tree |
      | parent cycle                   |
      | category without a name        |

  Scenario: A tree version below 1 is refused
    When the category tree is announced at version 0 with:
      | home | | Home |
    Then the fact is refused as invalid
    And the category list is empty at version 0

  Scenario: A new tree drops the categories it no longer lists and the product loses their path
    Given the category tree "Home > Kettles" is published
    And a purchasable product "TD-1" titled "Dropped Kettle" priced 3000 in category "kettles"
    When the category tree is published at version 7 with:
      | home | | Home |
    Then the product "TD-1" is reported purchasable and in stock
    And the shopper opens the product "TD-1"
    And the response has no field "categoryPath"
    And the category "home" has 0 purchasable products

  Scenario: The category list names the parent of every node and is ordered by path
    Given the category tree "Home > Kettles" is published
    When the shopper requests "/categories"
    Then the response field "version" is 5
    And the response field "categories.0.categoryId" is "home"
    And the response field "categories.0.parentId" is null
    And the response field "categories.1.parentId" is "home"
    And the response field "categories.1.path" is "Home/Kettles"

  Scenario: A hit shows its primary image and its category path
    Given the category tree "Home > Kettles" is published
    And the item master creates "HI-1" titled "Imaged Kettle"
    And a content draft described "A pictured kettle"
    And the draft has the GALLERY image "img-g"
    And the draft has the PRIMARY image "img-p"
    And product-content publishes the draft as revision 1 for "HI-1" in category "kettles"
    When the shopper searches for "imaged"
    Then the hit "HI-1" shows the primary image "img-p"
    And the hit "HI-1" has the category path "Home > Kettles"
