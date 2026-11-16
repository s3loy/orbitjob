const switches = [...document.querySelectorAll('[data-language-switch]')];
let selectedHash = window.location.hash;
const update = () => switches.forEach((link) => { link.hash = selectedHash; });
update();

// Remember the selected section before returning to the header. Scalar updates
// the URL while scrolling, including the scroll needed to reach this control.
document.addEventListener('click', (event) => {
  if (!(event.target instanceof Element)) return;
  const link = event.target.closest('a');
  if (!link) return;
  if (switches.includes(link)) {
    link.hash = selectedHash || window.location.hash;
    return;
  }
  const destination = new URL(link.href, window.location.href);
  if (destination.origin === window.location.origin && destination.pathname === window.location.pathname && destination.hash) {
    selectedHash = destination.hash;
    update();
  }
}, true);
window.addEventListener('popstate', () => {
  selectedHash = window.location.hash;
  update();
});
