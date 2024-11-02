# email_deleter
My Gmail account is filling up fast, so I created this program to clear out some spam, and free up some storage.

This program uses the Gmail API to order senders by the number of emails they have sent, and give the user the option to delete all emails from them.

## Running Instructions
Running this project requires a Google Cloud project to be set up. This can be done using the following instructions:
* Create a Google Cloud project as outlined here: ```https://developers.google.com/workspace/guides/create-project```
* Enable the Gmail API as outlined here: ```https://developers.google.com/workspace/guides/enable-apis```
* Under the OAuth consent screen tab in the Google Cloud console, add the email you wish to operate on as a test account
* Create access credentials as outlined here: ```https://developers.google.com/workspace/guides/create-credentials```
* Store the aforementioned credentials in the ```credentials.json``` file in the project root in JSON format
* In the Google Cloud console, add ```http://localhost:8080/callback``` as an authorised redirect URI

Now the Google Cloud project should be good to go.

This program is written in Go, which can be downloaded from here: https://go.dev/doc/install

Finally, run ```go build .``` and ```go run .``` from the project root, and follow the onscreen instructions!
