# Deployment Documentation

This project uses GitHub Actions for automated deployment to production servers via SSH.

## Architecture

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   GitHub        │────>│   Production    │────>│   Docker        │
│   Repository    │     │   Server        │     │   Compose       │
└─────────────────┘     └─────────────────┘     └─────────────────┘
```

## Prerequisites

### Server Setup

1. **Create a dedicated deploy user** (recommended):
   ```bash
   sudo useradd -m -s /bin/bash deploy
   sudo usermod -aG docker deploy
   ```

2. **Setup SSH keys**:
   ```bash
   sudo mkdir -p /home/deploy/.ssh
   # Add your public key to /home/deploy/.ssh/authorized_keys
   sudo chmod 700 /home/deploy/.ssh
   sudo chmod 600 /home/deploy/.ssh/authorized_keys
   sudo chown -R deploy:deploy /home/deploy/.ssh
   ```

3. **Disable password authentication** for the deploy user (optional but recommended):
   ```bash
   echo "Match User deploy" | sudo tee -a /etc/ssh/sshd_config
   echo "    PasswordAuthentication no" | sudo tee -a /etc/ssh/sshd_config
   sudo systemctl restart sshd
   ```

### GitHub Secrets Setup

Go to your repository Settings > Secrets and variables > Actions and add:

| Secret | Description | Example |
|--------|-------------|---------|
| `SSH_HOST` | Server IP address or hostname | `192.168.1.100` |
| `SSH_USER` | SSH username | `deploy` |
| `SSH_PORT` | SSH port (default: 22) | `22` |
| `SSH_PRIVATE_KEY` | SSH private key content | Full key with BEGIN/END |
| `SSH_KNOWN_HOSTS` | Server host key for verification | Output of `ssh-keyscan your-server.com` |
| `ENV_FILE_BASE64` | Base64-encoded .env file | `base64 -w 0 .env` |

### Generate SSH Keys

```bash
ssh-keygen -t ed25519 -C "github-actions@yourdomain.com" -f deploy_key
```

Copy the public key to your server's `~/.ssh/authorized_keys` and add the private key content to GitHub Secrets.

### Generate Host Key

```bash
ssh-keyscan your-server.com
```

Add the output to `SSH_KNOWN_HOSTS` secret.

## Deployment Trigger

The workflow is triggered on:
- Push to `main` branch
- Manual `workflow_dispatch` (via GitHub Actions UI)

## Deployment Process

1. **Checkout code** - Pulls repository to runner
2. **SSH setup** - Configures SSH agent with private key
3. **Host verification** - Adds server to known_hosts
4. **Deployment** - SSH into server and execute deployment script:
   - Save current commit for rollback
   - Pull latest code
   - Update .env file
   - Pull latest Docker images
   - Restart services
   - Health check verification
   - Cleanup old images

## Rollback

If any step fails, the script automatically rolls back:
- Restores previous git commit
- Restarts previous Docker images

## Health Checks

The workflow performs a health check on the Python service (config fetcher):
- Tries to connect to `http://localhost:8080/health`
- Retries up to 30 times (5 minutes total)
- Fails deployment if health check doesn't pass

## Notifications

- **Discord**: Configure `DISCORD_WEBHOOK` secret for failure notifications

## Manual Deployment

Trigger manually via GitHub Actions:
1. Go to Actions tab
2. Select "Deploy to Production"
3. Click "Run workflow" dropdown
4. Select branch and click "Run workflow"

## Monitoring

Deployment logs are saved to `/var/log/nginx-3x-ui-subscription-proxy_deploy.log` on the server.

## Troubleshooting

### SSH Connection Failed
- Verify SSH key is correct
- Check server firewall allows SSH port
- Verify user has proper permissions

### Health Check Failed
- Check Docker logs: `docker compose logs -f python`
- Verify Python service is running: `docker compose ps`
- Test endpoint directly: `docker compose exec python curl http://localhost:8080/health`

### Deployment Still Broken
- SSH into server and check logs: `tail -f /var/log/nginx-3x-ui-subscription-proxy_deploy.log`
- Manually rollback: `git checkout <previous-commit>` and `docker compose up -d`

## Security Best Practices

1. **Use dedicated deploy user** - Never use root
2. **SSH keys without passphrase** - Required for automation
3. **Host key verification** - Protects against MITM attacks
4. **Environment file in secrets** - Keeps sensitive data out of git
5. **Health checks** - Ensures deployment actually works
6. **Automatic rollback** - Minimizes downtime on failure
