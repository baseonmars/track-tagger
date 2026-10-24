# Windows release signing

Windows code signing is planned for future releases through Azure Artifact Signing PublicTrust Basic with an individual publisher identity. The Basic account in East US and GitHub OIDC identity are configured; individual identity validation, the PublicTrust certificate profile, its profile-scoped signer grant, and the verified publisher subject are still pending. Releases already published are unsigned; do not treat the presence of this guide or a workflow change as proof that a particular download is signed. Check the signature on the downloaded file.

The release flow builds unsigned artifacts first. After `github-release` approval, it signs the Windows CLI and desktop executables before packaging the ZIP, and signs the generated uninstaller and installer. It then verifies each signature, including the expected publisher and timestamp, before the signed Windows ZIP and installer can be published. Pull-request Windows artifacts remain explicitly marked unsigned.

## One-time Azure setup

1. Complete the publisher identity validation required for an Azure Artifact Signing **PublicTrust Basic** account using the intended individual identity. Use the configured Basic account in East US and create a PublicTrust certificate profile after validation. Record the profile's exact leaf-certificate subject as the publisher value; a display name or account name is not a substitute.
2. Use the configured user-assigned managed identity for GitHub Actions. After creating the profile, assign this identity only the role needed to sign with the intended certificate profile, scoped to that profile. Do not grant account-wide or subscription-wide signing access.
3. Add a federated identity credential to the managed identity with issuer `https://token.actions.githubusercontent.com`, audience `api://AzureADTokenExchange`, and subject `repo:baseonmars/track-tagger:environment:github-release`. This limits the credential to jobs using that GitHub environment. The release workflow requests an OIDC token after environment approval; it does not need a stored client secret.

## GitHub release environment

Protect the `github-release` environment with Dan as required reviewer, disable administrator bypass, and restrict deployments to `main` and the intended `v*` tags. The sole maintainer may approve a run he initiated; retain that existing setting. Keep the environment approval in place for both normal releases and manual version-tag releases. Approval to merge a signing change is separate from approval for its first release rollout; review and approve the first signed release explicitly after Azure validation and configuration are complete.

Set these required GitHub environment variables to the values from the validated Azure setup:

| Variable | Value |
| --- | --- |
| `AZURE_CLIENT_ID` | User-assigned managed identity's client ID |
| `AZURE_TENANT_ID` | Entra tenant ID |
| `AZURE_SUBSCRIPTION_ID` | Azure subscription containing the Artifact Signing account |
| `ARTIFACT_SIGNING_ENDPOINT` | Azure Artifact Signing endpoint for the account's region |
| `ARTIFACT_SIGNING_ACCOUNT` | Artifact Signing account name |
| `ARTIFACT_SIGNING_PROFILE` | Certificate profile name |
| `ARTIFACT_SIGNING_PUBLISHER` | Exact leaf-certificate subject for the validated individual publisher |

Do not start a release until all seven variables are present and the federated credential, profile-scoped role assignment, and environment protection are verified. Signing must run only after `github-release` approval. The workflow checks trust, exact publisher, and timestamp before uploading signed artifacts; publication generates checksums from those final assets. The signed x64 installer is installed and uninstalled only on a disposable GitHub runner, where installed executable and uninstaller signatures are also checked. ARM64 payloads are cross-compiled and signature-checked on x64; native ARM64 installation and desktop launch need separate acceptance on an ARM64 machine. Never approve or bypass the protected release gate without Dan's explicit signoff for that release.

The account, managed identity, and federated credential already exist. Environment variables are configured except `ARTIFACT_SIGNING_PUBLISHER`; the intended `public-release` profile is not yet created and the identity has no signing role. To finish setup, complete individual validation in the Azure portal, create the PublicTrust profile, grant **Artifact Signing Certificate Profile Signer** only at that profile's resource scope, and obtain its exact certificate subject before setting the publisher variable. Until then, production signing fails closed. Do not substitute a guessed name or a self-signed certificate.

Microsoft's [SignTool integration](https://learn.microsoft.com/en-us/azure/artifact-signing/how-to-signing-integrations) documents the regional endpoint, Azure CLI credentials, and SHA-256 timestamping. Inno Setup's [SignTool](https://jrsoftware.org/is6help/topic_setup_signtool.htm) and [SignedUninstaller](https://jrsoftware.org/ishelp/topic_setup_signeduninstaller.htm) settings cover signing the installer and embedded uninstaller. Signing does not guarantee that Windows SmartScreen immediately recognizes a new publisher.
