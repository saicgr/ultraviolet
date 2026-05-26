/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        background: "hsl(240 10% 4%)",
        foreground: "hsl(0 0% 98%)",
        muted: "hsl(240 4% 16%)",
        accent: "hsl(263 80% 60%)",
        border: "hsl(240 4% 22%)",
      },
    },
  },
  plugins: [],
};
