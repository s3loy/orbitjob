// Public Pages builds share the UI but never accept installation credentials.
export const publicDemo = import.meta.env.MODE === 'pages';
export const fixtureProfile = import.meta.env.MODE === 'fixtures';
export const projectHome = import.meta.env.BASE_URL.replace(/demo\/$/, '');
