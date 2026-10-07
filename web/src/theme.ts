import { createTheme } from "@mantine/core";

/** The plane the cards sit on: a light grey in the light theme, so that they stand out. */
export const pageBackground = "light-dark(var(--mantine-color-gray-0), var(--mantine-color-dark-8))";

// The look of the product: a neutral, dense, business-like interface. Colours
// carry state (green, red, orange) and never decoration.
export const theme = createTheme({
  primaryColor: "blue",
  defaultRadius: "md",
  fontFamily: 'system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif',
  fontFamilyMonospace: 'ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", monospace',
  headings: { fontWeight: "600" },
  components: {
    Table: { defaultProps: { highlightOnHover: true, verticalSpacing: "sm", horizontalSpacing: "md" } },
    Paper: { defaultProps: { withBorder: true, radius: "md" } },
    Card: { defaultProps: { withBorder: true, radius: "md" } },
    Button: { defaultProps: { fw: 500 } },
  },
});
