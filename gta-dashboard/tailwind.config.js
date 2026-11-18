export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      fontFamily: {
        // Public landing page only; the dashboard keeps the system font.
        marker: ['"Permanent Marker"', "cursive"],
        barlow: ['"Barlow"', "system-ui", "sans-serif"],
        "barlow-condensed": ['"Barlow Condensed"', '"Barlow"', "sans-serif"],
      },
    },
  },
  plugins: [],
}
