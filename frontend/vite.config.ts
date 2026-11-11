import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";
export default defineConfig({
  resolve: { tsconfigPaths: true },
  build: {
    rolldownOptions: {
      output: {
        codeSplitting: {
          groups: [
            { name: "react-vendor", test: /node_modules\/(react|react-dom|scheduler)\// },
            { name: "radix-vendor", test: /node_modules\/@radix-ui\// },
          ],
        },
      },
    },
  },
  plugins: [react(), tailwindcss()],
});
