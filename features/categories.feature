Feature: Browsing the category tree
  The category view is the last published tree, with purchasable counts rolled
  up from the products. A rename needs no re-index: product rows keep only the
  category id and the path is joined at query time.

  Scenario: Counts roll up to the parent category
    Given the category tree "Home > Kettles" is published
    And a purchasable product "K1" titled "Steel Kettle" priced 3000 in category "kettles"
    Then the category "home" has 1 purchasable products
    And the category "kettles" has 1 purchasable products

  Scenario: A renamed category shows at once on its products
    Given the category tree "Home > Kettles" is published
    And a purchasable product "K1" titled "Steel Kettle" priced 3000 in category "kettles"
    When the category tree is republished as version 6 with "kettles" renamed "Boilers"
    Then the product "K1" has the category path "Home > Boilers"

  Scenario: An older tree version is ignored
    Given the category tree "Home > Kettles" is published
    When the category tree is republished as version 2 with "kettles" renamed "Stale"
    Then the product catalogue still shows the category "kettles" named "Kettles"

  Scenario: Before the first tree the list is empty
    Then the category list is empty at version 0
