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
    // The Monaco runtime, copied in from node_modules by
    // scripts/sync-monaco.mjs. It is a vendored build, not source: linting it
    // buries every real finding under twenty-five thousand from minified code
    // nobody here wrote.
    "public/monaco/**",
  ]),
]);

export default eslintConfig;
