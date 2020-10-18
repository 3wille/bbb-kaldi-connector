# bbb-kaldi-connector

This repository is the result of my master programm project at the [LT Group](https://github.com/uhh-lt) at the Univeristy of Hamburg.


This project relays audio from freeswitch to a [Kaldi-Model-Server](https://github.com/uhh-lt/kaldi-model-server) over redis. Audio is received over RTP as OPUS, decoded and send on as PCM 16bit.

## Usage 

```
go run bbb_secret_path_token sentry_token
```
