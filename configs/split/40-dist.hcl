dist {
  gradle {
    registry = "https://services.gradle.org/distributions"
    mirrors = ["https://dist-cache.example.com/gradle"]
    mirror_policy = "ordered"
    tag_ttl = "24h"
    allow = ["gradle-*-bin.zip", "gradle-*-all.zip"]
  }

  electron {
    registry = "https://github.com/electron/electron/releases/download"
    mirrors = ["https://dist-cache.example.com/electron"]
    mirror_policy = "ordered"
    allow = [
      "v*/electron-v*",
      "v*/SHASUMS256.txt",
      "v*/SHASUMS256.txt.sig",
    ]
  }

  playwright {
    registry = "https://cdn.playwright.dev"
    mirrors = ["https://dist-cache.example.com/playwright"]
    mirror_policy = "ordered"
    allow = ["builds/*", "dbazure/download/playwright/*"]
  }

  nodejs {
    registry = "https://nodejs.org/download/release"
    mirrors = ["https://dist-cache.example.com/nodejs"]
    mirror_policy = "ordered"
    allow = ["v*/node-v*", "index.json", "index.tab"]
  }
}
