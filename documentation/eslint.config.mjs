import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

const config = [...nextVitals, ...nextTs, { ignores: [".next/**", "out/**", ".source/**", "node_modules/**", "public/**", "scripts/**", "next-env.d.ts", ".snippet-check/**", "playwright-report/**", "test-results/**"] }];

export default config;
