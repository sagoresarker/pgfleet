/* PgFleet site — progressive enhancement only.
   The site is fully functional with JavaScript disabled. */
(function () {
  "use strict";

  // Mark that JS is available (un-hides reveal targets gracefully if observer missing).
  document.documentElement.classList.remove("no-js");

  /* ---- Mobile nav toggle -------------------------------------------------- */
  var toggle = document.querySelector(".nav-toggle");
  var links = document.getElementById("nav-links");
  if (toggle && links) {
    toggle.addEventListener("click", function () {
      var open = links.classList.toggle("open");
      toggle.setAttribute("aria-expanded", open ? "true" : "false");
    });
    // Close menu when a link is chosen.
    links.addEventListener("click", function (e) {
      if (e.target.closest("a")) {
        links.classList.remove("open");
        toggle.setAttribute("aria-expanded", "false");
      }
    });
  }

  /* ---- Copy-to-clipboard for code blocks (progressive enhancement) -------- */
  /* The <pre> is fully readable/selectable without JS; this just adds a button. */
  document.querySelectorAll(".code-copy[data-copy]").forEach(function (btn) {
    var block = btn.closest(".code-block");
    var pre = block && block.querySelector("pre");
    if (!pre) return;
    btn.addEventListener("click", function () {
      var text = pre.innerText.replace(/\n+$/, "");
      var label = btn.querySelector(".label");
      var done = function () {
        btn.classList.add("copied");
        if (label) label.textContent = "Copied";
        setTimeout(function () {
          btn.classList.remove("copied");
          if (label) label.textContent = "Copy";
        }, 1600);
      };
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(done, function () {});
      } else {
        try {
          var ta = document.createElement("textarea");
          ta.value = text;
          ta.setAttribute("readonly", "");
          ta.style.position = "absolute";
          ta.style.left = "-9999px";
          document.body.appendChild(ta);
          ta.select();
          document.execCommand("copy");
          document.body.removeChild(ta);
          done();
        } catch (e) { /* no-op: text is still selectable manually */ }
      }
    });
  });

  /* ---- Reveal on scroll --------------------------------------------------- */
  var reveals = document.querySelectorAll(".reveal");
  var reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  if (reduce || !("IntersectionObserver" in window)) {
    reveals.forEach(function (el) { el.classList.add("is-in"); });
    return;
  }

  var io = new IntersectionObserver(function (entries) {
    entries.forEach(function (entry) {
      if (entry.isIntersecting) {
        entry.target.classList.add("is-in");
        io.unobserve(entry.target);
      }
    });
  }, { rootMargin: "0px 0px -8% 0px", threshold: 0.08 });

  reveals.forEach(function (el) { io.observe(el); });
})();
