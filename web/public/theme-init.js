(function () {
  try {
    var validThemes = ['burned-paper', 'vivid-yellow-green', 'cute-pastel-blue', 'bioluminescent'];
    var validModes = ['light', 'dark'];
    var rawTheme = localStorage.getItem('pulse_ui_theme');
    var rawMode = localStorage.getItem('pulse_ui_mode');
    var theme = validThemes.indexOf(rawTheme) !== -1 ? rawTheme : 'bioluminescent';
    var mode = validModes.indexOf(rawMode) !== -1 ? rawMode : 'dark';
    document.documentElement.setAttribute('data-theme', theme);
    document.documentElement.setAttribute('data-mode', mode);
    var colors = {
      bioluminescent: { dark: '#0a0a0c', light: '#f6f7f9' },
      'burned-paper': { dark: '#161311', light: '#f5efe6' },
      'vivid-yellow-green': { dark: '#0c120e', light: '#f8faf6' },
      'cute-pastel-blue': { dark: '#0c131a', light: '#f3f7fb' },
    };
    var meta = document.querySelector('meta[name="theme-color"]');
    if (meta && colors[theme] && colors[theme][mode]) {
      meta.setAttribute('content', colors[theme][mode]);
    }
  } catch (_e) {}
})();
