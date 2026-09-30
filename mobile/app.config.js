// The app's version follows dsync's (wails.json), so a phone build and the
// desktop release it shipped with have the same number.
const fs = require('fs');
const path = require('path');

module.exports = ({ config }) => {
  let version = config.version;
  try {
    const wails = JSON.parse(fs.readFileSync(path.join(__dirname, '..', 'wails.json'), 'utf8'));
    version = wails.info.productVersion || version;
  } catch {
    // Outside the dsync repo: keep app.json's version.
  }
  // 1.2.3 -> 10203; Android needs a growing number to install an update.
  const [major = 0, minor = 0, patch = 0] = version.split('.').map((n) => parseInt(n, 10) || 0);
  const build = major * 10000 + minor * 100 + patch;
  return {
    ...config,
    version,
    ios: { ...config.ios, buildNumber: String(build) },
    android: { ...config.android, versionCode: build },
  };
};
