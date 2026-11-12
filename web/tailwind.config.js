export default {
  content: ["./index.html", "./src/**/*.{vue,ts}"],
  corePlugins: {
    preflight: false,
  },
  theme: {
    extend: {
      colors: {
        cyan: "#61e0d1",
        fuchsia: "#ff4ecd",
        ink: "#0a1016",
      },
    },
  },
};
