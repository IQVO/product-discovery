Feature: Searching and browsing products
  Search ranks title, category names, attributes and description, falls back to
  trigram similarity for typos, and answers facets over the whole result.

  Background:
    Given the category tree "Home > Kettles" is published
    And a purchasable product "K1" titled "Steel Kettle" priced 3000 in category "kettles"
    And a purchasable product "K2" titled "Glass Kettle" priced 8000 in category "kettles"
    And a purchasable product "G1" titled "Garden Hose" priced 12000 in category "home"

  Scenario: Full-text search finds products by title
    When the shopper searches for "kettle"
    Then the hits are "K1, K2" in any order

  Scenario: A typo falls back to trigram similarity
    When the shopper searches for "ketle"
    Then the hits are "K1, K2" in any order

  Scenario: A product-type query suggests categories
    When the shopper searches for "kettles"
    Then the category suggestions are "kettles"

  Scenario: Filters combine with AND
    When the shopper searches with price from 5000 to 20000 sorted by "price-asc"
    Then the hits are exactly "K2, G1"

  Scenario: The category filter covers the subtree
    When the shopper searches inside category "home" sorted by "title"
    Then the hits are exactly "G1, K2, K1"

  Scenario: Facets count the whole result, not the page
    When the shopper searches with page size 1
    Then there are 3 hit-independent facets
    And the price-band facet counts "<2500" as 0 and "2500-4999" as 1

  Scenario: Pages follow the cursor without gaps
    When the shopper pages through everything sorted by "title" 1 at a time
    Then the pages concatenate to "G1, K2, K1" in order

  Scenario: Bad parameters are a validation failure
    When the shopper searches with sort "cheapest"
    Then the answer is 422 "validation-failed"
