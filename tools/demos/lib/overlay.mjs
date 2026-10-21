// The in-page layer that makes a headless recording read like a person using
// the product: a cursor that follows Playwright's mouse, a ripple on every
// click, and a caption bar. Headless Chromium paints no cursor of its own, so
// without this the clicks in a video happen with nothing on screen causing
// them.
//
// The script is installed with context.addInitScript, so it runs again on every
// document the page loads. The last cursor position is kept in sessionStorage
// so a full navigation does not snap the cursor back to the corner.

function install() {
  const POS_KEY = "__demoCursor";
  const Z = "2147483647";

  const style = document.createElement("style");
  style.textContent = `
    #__demo-cursor {
      position: fixed; left: 0; top: 0; width: 24px; height: 24px;
      pointer-events: none; z-index: ${Z};
      filter: drop-shadow(0 1px 2px rgba(0, 0, 0, 0.35));
      transition: transform 90ms ease-out;
    }
    #__demo-cursor.down { transform: scale(0.85); }
    .__demo-ripple {
      position: fixed; width: 36px; height: 36px; margin: -18px 0 0 -18px;
      border-radius: 50%; pointer-events: none; z-index: ${Z};
      background: rgba(59, 89, 117, 0.28);
      animation: __demo-ripple 520ms ease-out forwards;
    }
    @keyframes __demo-ripple {
      from { transform: scale(0.3); opacity: 1; }
      to { transform: scale(1.6); opacity: 0; }
    }
    #__demo-caption {
      position: fixed; left: 50%; bottom: 40px; transform: translate(-50%, 12px);
      max-width: 70%; padding: 14px 24px; border-radius: 12px;
      background: rgba(20, 24, 31, 0.88); color: #f4f2ee;
      font: 500 20px/1.35 Inter, system-ui, sans-serif; letter-spacing: -0.01em;
      text-align: center; pointer-events: none; z-index: ${Z};
      opacity: 0; transition: opacity 260ms ease, transform 260ms ease;
    }
    #__demo-caption.shown { opacity: 1; transform: translate(-50%, 0); }

    /* Hades checkboxes hide the real input with sr-only, which positions it
       absolutely against the nearest positioned ancestor. Inside the agent
       builder that is the modal frame, so headless Chromium scrolls the
       overflow-hidden frame to the focused input on click and the modal goes
       blank. Positioning the label keeps the input inside its list. It changes
       nothing visible. */
    label:has(> input.sr-only) { position: relative; }
  `;

  const cursor = document.createElement("div");
  cursor.id = "__demo-cursor";
  cursor.innerHTML = `<svg width="24" height="24" viewBox="0 0 24 24">
    <path d="M5 3l14 8.2-6.1 1.3-3.3 5.5z" fill="#111" stroke="#fff" stroke-width="1.5"
      stroke-linejoin="round"/></svg>`;

  const caption = document.createElement("div");
  caption.id = "__demo-caption";

  const place = (x, y) => {
    cursor.style.left = `${x - 5}px`;
    cursor.style.top = `${y - 3}px`;
  };
  const saved = JSON.parse(sessionStorage.getItem(POS_KEY) ?? "null");
  place(saved?.x ?? -40, saved?.y ?? -40);

  // Capture phase, so an app handler that stops propagation cannot hide the
  // cursor's movement or a click from the recording.
  window.addEventListener(
    "mousemove",
    (e) => {
      place(e.clientX, e.clientY);
      sessionStorage.setItem(POS_KEY, JSON.stringify({ x: e.clientX, y: e.clientY }));
    },
    true,
  );
  window.addEventListener(
    "mousedown",
    (e) => {
      cursor.classList.add("down");
      const ripple = document.createElement("div");
      ripple.className = "__demo-ripple";
      ripple.style.left = `${e.clientX}px`;
      ripple.style.top = `${e.clientY}px`;
      document.documentElement.appendChild(ripple);
      setTimeout(() => ripple.remove(), 600);
    },
    true,
  );
  window.addEventListener("mouseup", () => cursor.classList.remove("down"), true);

  window.__demoCaption = (text) => {
    if (text) caption.textContent = text;
    caption.classList.toggle("shown", Boolean(text));
  };

  const mount = () => document.documentElement.append(style, cursor, caption);
  if (document.documentElement) mount();
  else document.addEventListener("DOMContentLoaded", mount, { once: true });
}

export const overlayScript = `(${install.toString()})();`;
