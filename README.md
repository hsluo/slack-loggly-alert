# slack-loggly-alert
Formats Loggly's http alerts into Slack attachments

Example of the alert:		
![alert](http://i.imgur.com/G45W1M6.png)

Or polls Loggly search API as alerts for free tier users. The result is not rich enough for attachments.

# Prerequisite
- Google App Engine SDK

# Usage
- Create a new project on GAE
- Copy `app.yaml.default` to `app.yaml`, `credentials.json.default` to `credentials.json` and fill in the blanks
- Deploy to GAE
- Config Loggly alert endpoint as your GAE app url
