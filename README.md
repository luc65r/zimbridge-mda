# Zimbridge-MDA

Zimbridge-MDA (Zimbra bridge, Mail Delivery Agent) uses your USERNAME and your
PASSWORD to connect to https://mail.etu.cyu.fr (Zimbra webmail instance) and
download all your e-mails.  It stores them in the provided MAILDIR directory,
using Maildir++ directory layout.  You can then use an email client to read your
e-mails offline, or configure an IMAP server like Dovecot to use that directory.
Zimbridge-MDA can also move all the stored e-mails to the trash folder in the
webmail, so that it doesn't fetch them again the next time.

## Usage

zimbridge-mda -username USERNAME -password PASSWORD -address ADDRESS MAILDIR
