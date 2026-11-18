const switches = [...document.querySelectorAll('[data-language-link]')];
let selectedHash = window.location.hash;
const update = () => switches.forEach((link) => {
  const target = new URL(link.href);
  target.search = window.location.search;
  target.hash = selectedHash;
  link.href = target.href;
});
update();

// Remember the selected section before returning to the header. Scalar updates
// the URL while scrolling, including the scroll needed to reach this control.
document.addEventListener('click', (event) => {
  if (!(event.target instanceof Element)) return;
  const link = event.target.closest('a');
  if (!link) return;
  if (switches.includes(link)) {
    update();
    return;
  }
  if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || link.hasAttribute('download')) return;
  const target = link.target.toLowerCase();
  if (target && target !== '_self') return;
  const destination = new URL(link.href, window.location.href);
  if (destination.origin === window.location.origin && destination.pathname === window.location.pathname && destination.hash) {
    selectedHash = destination.hash;
    update();
  }
}, true);
const rememberLocation = () => {
  selectedHash = window.location.hash;
  update();
};
window.addEventListener('hashchange', rememberLocation);
window.addEventListener('popstate', rememberLocation);
// Programmatic selections push history; ignore the viewer's scroll replacements.
if (window.navigation) {
  window.navigation.addEventListener('currententrychange', (event) => {
    if (event.navigationType === 'push') rememberLocation();
  });
} else {
  const pushState = window.history.pushState;
  window.history.pushState = function (...args) {
    const result = pushState.apply(this, args);
    rememberLocation();
    return result;
  };
}
