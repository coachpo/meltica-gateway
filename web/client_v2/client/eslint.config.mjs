import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  // Override default ignores of eslint-config-next.
  globalIgnores([
    // Default ignores of eslint-config-next:
    ".next/**",
    "out/**",
    "build/**",
    "next-env.d.ts",
  ]),
  {
    files: ["src/**/*.ts", "src/**/*.tsx"],
    rules: {
      "no-restricted-globals": [
        "error",
        {
          name: "fetch",
          message: "Use the shared API modules in src/lib/api instead of calling fetch directly.",
        },
      ],
    },
  },
  {
    files: ["src/lib/api/**/*.ts"],
    rules: {
      "no-restricted-globals": "off",
    },
  },
]);

export default eslintConfig;
