# Configuration files created for GitHub Actions deployment

## Files Created

1. **`.github/workflows/deploy.yml`** - Main GitHub Actions workflow
   - Triggers on push to main branch
   - SSH deployment with host verification
   - Automatic rollback on failure
   - Health check verification
   - Old image cleanup

2. **`deploy.sh`** - Server-side deployment script
   - Standalone script that can be run manually or via workflow
   - Full rollback logic
   - Health checks
   - Logging

3. **`DEPLOYMENT.md`** - Comprehensive deployment documentation
   - Server setup instructions
   - GitHub Secrets configuration
   - Troubleshooting guide

## Quick Start

1. Generate SSH key: `ssh-keygen -t ed25519 -C "github-actions@yourdomain.com"`
2. Add public key to server's `~/.ssh/authorized_keys`
3. Add private key to GitHub Secrets as `SSH_PRIVATE_KEY`
4. Get host key: `ssh-keyscan your-server.com` → add to `SSH_KNOWN_HOSTS`
5. Encode .env: `base64 -w 0 .env` → add to `ENV_FILE_BASE64`
6. Configure GitHub Secrets in repository settings

## Server Setup (Run Once)

```bash
# Create deploy user
sudo useradd -m -s /bin/bash deploy
sudo usermod -aG docker deploy

# Setup SSH
sudo mkdir -p /home/deploy/.ssh
echo "YOUR_PUBLIC_KEY" | sudo tee /home/deploy/.ssh/authorized_keys
sudo chmod 700 /home/deploy/.ssh
sudo chmod 600 /home/deploy/.ssh/authorized_keys
sudo chown -R deploy:deploy /home/deploy/.ssh
```

## Usage

- **Automatic**: Push to main branch → auto-deploys
- **Manual**: GitHub Actions → "Deploy to Production" → Run workflow
