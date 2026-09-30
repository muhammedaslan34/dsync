// Signs Android release builds with dsync's own key when DSYNC_KEYSTORE
// (and DSYNC_KEYSTORE_PASSWORD, DSYNC_KEY_ALIAS, DSYNC_KEY_PASSWORD) are
// set, as in the release workflow. Without them release builds keep
// Expo's debug key, which is fine for trying a build but can't update an
// app installed from a real release.
const { withAppBuildGradle } = require('expo/config-plugins');

const releaseConfig = `
        release {
            if (System.getenv('DSYNC_KEYSTORE')) {
                storeFile file(System.getenv('DSYNC_KEYSTORE'))
                storePassword System.getenv('DSYNC_KEYSTORE_PASSWORD')
                keyAlias System.getenv('DSYNC_KEY_ALIAS')
                keyPassword System.getenv('DSYNC_KEY_PASSWORD')
            }
        }`;

module.exports = function withReleaseSigning(config) {
  return withAppBuildGradle(config, (config) => {
    let src = config.modResults.contents;
    if (src.includes("System.getenv('DSYNC_KEYSTORE')")) return config;

    const configs = /signingConfigs\s*\{/;
    const release = /(buildTypes\s*\{[\s\S]*?release\s*\{[\s\S]*?)signingConfig\s+signingConfigs\.debug/;
    if (!configs.test(src) || !release.test(src)) {
      throw new Error('withReleaseSigning: android/app/build.gradle has an unexpected layout');
    }
    src = src.replace(configs, (m) => m + releaseConfig);
    src = src.replace(
      release,
      "$1signingConfig System.getenv('DSYNC_KEYSTORE') ? signingConfigs.release : signingConfigs.debug",
    );
    config.modResults.contents = src;
    return config;
  });
};
