# Security

msoshot opens whatever it is sent in your real Outlook profile, so treat its port like a remote desktop:

- Keep it on `127.0.0.1` or a private network address. Never expose it to the internet.
- The token is the only access control. Replace it from the dashboard if it leaks.
- The app only opens and displays messages. It never sends, saves or moves mail.

To report a vulnerability, use GitHub's private reporting: **Security → Report a vulnerability** on this repository. Please don't open a public issue.
