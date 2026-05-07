# Mail Client Coverage

This table tracks expected mail-client discovery support and manual verification
status. `Support` means the client is expected to work through one of the
discovery mechanisms implemented by wellknown-overlay. `Tested In Software`
means the behavior has been manually verified in that client.

| Software | Mechanism | Support | Tested In Software |
|---|---|---|---|
| Thunderbird desktop | Thunderbird Autoconfig | Yes | No |
| Thunderbird Android / K-9 Mail | Thunderbird Autoconfig | Yes | No |
| FairEmail Android | Thunderbird Autoconfig | Probable | No |
| Outlook desktop | Outlook Autodiscover | Yes | No |
| Outlook mobile | Outlook Autodiscover | Probable | No |
| Apple Mail macOS | Apple mobileconfig | Yes | No |
| Apple Mail iOS / iPadOS | Apple mobileconfig | Yes | No |
| Samsung Email Android | Outlook Autodiscover or manual IMAP | Probable | No |
| Gmail Android | Manual IMAP / provider-specific flows | No | No |
