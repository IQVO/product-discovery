# rest_rules.feature — documentation-derived scenarios for the REST contract (apis/openapi.yaml,
# .claude/rules/rest-api.md, troubleshooting "Search and the API"): the paging parameters,
# the problem types, the read-only surface and the cache validators.
Feature: The read-only REST contract

  Scenario Outline: A page size from 1 to 500 is accepted
    When the shopper requests "/products?limit=<limit>"
    Then the answer status is 200

    Examples:
      | limit |
      | 1     |
      | 500   |

  Scenario Outline: A page size outside 1 to 500, empty, repeated or not a number is an invalid limit
    When the shopper requests "/products?<query>"
    Then the answer is 400 "invalid-limit"

    Examples:
      | query           |
      | limit=0         |
      | limit=501       |
      | limit=-1        |
      | limit=abc       |
      | limit=          |
      | limit=1&limit=2 |

  Scenario Outline: A parameter the operation does not declare is refused instead of ignored
    When the shopper requests "/products?<query>"
    Then the answer is 400 "unknown-query-parameter"

    Examples:
      | query         |
      | after=abc     |
      | offset=20     |
      | nextToken=abc |

  Scenario Outline: A cursor that is repeated or is not a cursor is an invalid cursor
    When the shopper requests "/products?<query>"
    Then the answer is 400 "invalid-cursor"

    Examples:
      | query               |
      | cursor=a&cursor=b   |
      | cursor=e30          |

  Scenario: A cursor of one sort cannot be replayed under another sort
    Given a purchasable product "CU-1" titled "Cursor Pot One" priced 3000 in category "kettles"
    And a purchasable product "CU-2" titled "Cursor Pot Two" priced 4000 in category "kettles"
    When the shopper requests "/products?sort=title&limit=1"
    And the shopper reuses the next cursor on "/products?sort=price-asc&limit=1"
    Then the answer is 400 "invalid-cursor"

  Scenario: A cursor of a browse cannot be replayed on a text search
    Given a purchasable product "CU-3" titled "Cursor Pot Three" priced 3000 in category "kettles"
    And a purchasable product "CU-4" titled "Cursor Pot Four" priced 4000 in category "kettles"
    When the shopper requests "/products?sort=title&limit=1"
    And the shopper reuses the next cursor on "/products?q=cursor&sort=title&limit=1"
    Then the answer is 400 "invalid-cursor"

  Scenario Outline: A NUL character is a client error and never reaches the database
    When the shopper requests "<path>"
    Then the answer is <status> "<slug>"

    Examples:
      | path              | status | slug               |
      | /products?q=ke%00 | 400    | malformed-request  |
      | /products/ab%00cd | 404    | resource-not-found |

  Scenario Outline: The surface is read-only, so no write verb is routed
    When a <method> request is sent to "<path>"
    Then the answer status is 405

    Examples:
      | method | path          |
      | POST   | /products     |
      | PUT    | /products/K-1 |
      | PATCH  | /products/K-1 |
      | DELETE | /categories   |

  Scenario: A product that does not exist answers an RFC 9457 problem
    When the shopper requests "/products/no-such-item"
    Then the answer is 404 "not-found"
    And the answer is served as problem+json
    And the response field "status" is 404
    And the response field "instance" is "/products/no-such-item"

  Scenario: A product read carries a strong ETag and may be cached for 30 seconds
    Given a purchasable product "E-1" titled "Etag Kettle"
    When the shopper reads "/products/E-1" noting its ETag
    Then the answer status is 200
    And the answer carries an ETag and may be cached for 30 seconds

  Scenario Outline: A validator that still matches is answered 304 without a body
    Given a purchasable product "E-2" titled "Revalidated Kettle"
    And the shopper reads "/products/E-2" noting its ETag
    When the shopper reads "/products/E-2" presenting <validator>
    Then the answer status is <status>

    Examples:
      | validator                     | status |
      | the noted ETag                | 304    |
      | a weak copy of the noted ETag | 304    |
      | the wildcard ETag             | 304    |
      | an unrelated ETag             | 200    |

  Scenario: A 304 repeats the validator and sends no body
    Given a purchasable product "E-3" titled "Empty Answer Kettle"
    And the shopper reads "/products/E-3" noting its ETag
    When the shopper reads "/products/E-3" presenting the noted ETag
    Then the answer has no body
    And the ETag equals the noted one

  Scenario: A new price version changes the ETag of the product
    Given a purchasable product "E-4" titled "Repriced Kettle"
    And the shopper reads "/products/E-4" noting its ETag
    When pricing sets version 2 of the price of "E-4" to 1900 with list price 2500
    And the shopper reads "/products/E-4" presenting the noted ETag
    Then the answer status is 200
    And the ETag differs from the noted one

  Scenario: A new category tree changes the ETag of the category list
    Given the category tree "Home > Kettles" is published
    And the shopper reads "/categories" noting its ETag
    When the category tree is republished as version 6 with "kettles" renamed "Boilers"
    And the shopper reads "/categories" presenting the noted ETag
    Then the answer status is 200
    And the ETag differs from the noted one
