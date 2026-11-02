import type { SidebarsConfig } from "@docusaurus/plugin-content-docs";

const sidebar: SidebarsConfig = {
  apisidebar: [
    {
      type: "doc",
      id: "api-reference/rest/product-discovery-api",
    },
    {
      type: "category",
      label: "Products",
      link: {
        type: "doc",
        id: "api-reference/rest/products",
      },
      items: [
        {
          type: "doc",
          id: "api-reference/rest/search-products",
          label: "Search and filter purchasable products",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api-reference/rest/get-product",
          label: "Get one product view",
          className: "api-method get",
        },
      ],
    },
    {
      type: "category",
      label: "Categories",
      link: {
        type: "doc",
        id: "api-reference/rest/categories",
      },
      items: [
        {
          type: "doc",
          id: "api-reference/rest/list-categories",
          label: "Get the category tree with purchasable counts",
          className: "api-method get",
        },
      ],
    },
  ],
};

export default sidebar.apisidebar;
