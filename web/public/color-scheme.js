// Applies the chosen colour scheme before the page paints, so that a person who
// picked the dark theme does not see a flash of the light one. It is a file of
// its own rather than an inline script, so that the page needs no 'unsafe-inline'
// for scripts. Light is the default; the switcher in the header changes it.
(function () {
  var scheme = "light";
  try {
    if (localStorage.getItem("mantine-color-scheme-value") === "dark") scheme = "dark";
  } catch (e) {
    // Storage is blocked: the light theme stays.
  }
  document.documentElement.setAttribute("data-mantine-color-scheme", scheme);
})();
