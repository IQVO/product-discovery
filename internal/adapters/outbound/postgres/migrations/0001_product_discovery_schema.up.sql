-- product-discovery read side: ProductView rows with their search document,
-- the one CategoryView, the ListingItemMap and the consumer inbox.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE products (
    catalog_item_id      text    PRIMARY KEY,
    title                text    NOT NULL DEFAULT '',
    title_sort           text    NOT NULL DEFAULT '',
    hazmat               boolean NOT NULL DEFAULT false,
    fragile              boolean NOT NULL DEFAULT false,
    item_state           text    NOT NULL DEFAULT '',
    content_state        text    NOT NULL DEFAULT '',
    content_revision     bigint  NOT NULL DEFAULT 0,
    description          text    NOT NULL DEFAULT '',
    bullets              jsonb   NOT NULL DEFAULT '[]',
    attributes           jsonb   NOT NULL DEFAULT '[]',
    media                jsonb   NOT NULL DEFAULT '[]',
    category_id          text    NOT NULL DEFAULT '',
    price_state          text    NOT NULL DEFAULT '',
    price_version        bigint  NOT NULL DEFAULT 0,
    currency             text    NOT NULL DEFAULT '',
    list_price_minor     bigint  NOT NULL DEFAULT 0,
    effective_price_minor bigint NOT NULL DEFAULT 0,
    listings             jsonb   NOT NULL DEFAULT '[]',
    -- derived by the fold rules, stored so search can filter and sort on them
    in_stock             boolean NOT NULL DEFAULT false,
    purchasable          boolean NOT NULL DEFAULT false,
    searchable           boolean NOT NULL DEFAULT false,
    has_price            boolean NOT NULL DEFAULT false,
    -- title A, attributes B, description and bullets C (category names are
    -- matched at query time: ADR 0002 A4)
    doc                  tsvector NOT NULL DEFAULT ''::tsvector
);

CREATE INDEX products_doc_idx ON products USING GIN (doc) WHERE searchable;
CREATE INDEX products_title_trgm_idx ON products USING GIN (title gin_trgm_ops) WHERE searchable;
CREATE INDEX products_category_idx ON products (category_id) WHERE searchable;
CREATE INDEX products_price_idx ON products (effective_price_minor, catalog_item_id) WHERE searchable AND has_price;
CREATE INDEX products_title_idx ON products (title_sort COLLATE "C", catalog_item_id) WHERE searchable;
CREATE INDEX products_purchasable_idx ON products (category_id) WHERE purchasable;

CREATE TABLE category_view (
    singleton  boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    version    bigint  NOT NULL,
    categories jsonb   NOT NULL
);

CREATE TABLE listing_items (
    listing_id      text PRIMARY KEY,
    catalog_item_id text NOT NULL
);

CREATE TABLE processed_events (
    event_id     text        PRIMARY KEY,
    event_type   text        NOT NULL,
    processed_at timestamptz NOT NULL DEFAULT now()
);
