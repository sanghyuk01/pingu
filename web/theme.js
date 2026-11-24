// Apply the theme before CSS loads so refreshes do not flash the wrong colors.
(() => {
  let saved;
  try {
    saved = localStorage.getItem("pingu-theme");
  } catch {}
  const dark =
    saved === "dark" ||
    (saved !== "light" && matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.dataset.theme = dark ? "dark" : "light";
})();
