# Signing the Windows installer

Windows shows **"Unknown publisher"** (and SmartScreen's "Windows protected your PC") for
programs that aren't signed with a code-signing certificate from a certificate authority
Windows trusts. `scripts/package.sh` signs `dsync.exe`, `dsync-gui.exe` and the installer
when `DSYNC_SIGN_CMD` is set: a command that signs the file given as its last argument, in
place. Without it, everything is built unsigned as before.

## Getting a certificate

A self-signed certificate doesn't help on other people's computers. Since 2023, new
code-signing certificates keep their private key in hardware or a cloud service, so
there's no `.pfx` to download for most of them. Options, cheapest first:

- **SignPath Foundation** (free for open-source projects): signs through SignPath's
  service from GitHub Actions. dsync needs an open-source license (a `LICENSE` file)
  to apply.
- **Certum Open Source Code Signing** (about €50 a year, for individuals): key in Certum's
  cloud (SimplySign), usable from Linux through PKCS#11 with `osslsigncode` or `jsign`.
- **Azure Artifact Signing** (formerly Trusted Signing, about $10 a month): only for
  organizations and for individuals in some countries. Sign with
  `jsign --storetype TRUSTEDSIGNING`.
- **An OV certificate** from Sectigo, SSL.com, DigiCert and others (about $200–400 a year).

An OV certificate replaces "Unknown publisher" with your name right away. SmartScreen
also learns the certificate's reputation as people download it.

## Examples

```sh
# A certificate in a .pfx file (password in DSYNC_SIGN_PASSWORD):
DSYNC_SIGN_CMD="packaging/sign/osslsigncode-pfx /path/to/cert.pfx" ./scripts/package.sh

# Azure Artifact Signing through jsign:
DSYNC_SIGN_CMD="jsign --storetype TRUSTEDSIGNING --keystore weu.codesigning.azure.net \
  --storepass $(az account get-access-token --resource https://codesigning.azure.net -q accessToken -o tsv) \
  --alias ACCOUNT/PROFILE --tsaurl http://timestamp.acs.microsoft.com" ./scripts/package.sh
```

The uninstaller (`uninstall.exe`, written by the installer) stays unsigned.
