# Mail Client Coverage

This table tracks expected mail-client discovery support and manual verification
status. `Support` means the client is expected to work through one of the
discovery mechanisms implemented by wellknown-overlay. `Tested In Software`
means the behavior has been manually verified in that client.

| Software | Mechanism | Support | Tested In Software |
|---|---|---|---|
| Thunderbird desktop | Thunderbird Autoconfig | Yes | Yes |
| Thunderbird Android / K-9 Mail | Thunderbird Autoconfig | Yes | No |
| FairEmail Android | Thunderbird Autoconfig | Probable | No |
| Outlook desktop | Outlook Autodiscover | Yes | Yes |
| Outlook mobile | Outlook Autodiscover | Probable | No |
| Apple Mail macOS add-account wizard | Unknown / Apple proprietary discovery | No | Yes |
| Apple Mail macOS configuration profile | Apple mobileconfig | Yes | Yes |
| Apple Mail iOS / iPadOS configuration profile | Apple mobileconfig | Probable | No |
| Samsung Email Android | Outlook Autodiscover or manual IMAP | Probable | No |
| Gmail Android | Manual IMAP / provider-specific flows | No | No |
