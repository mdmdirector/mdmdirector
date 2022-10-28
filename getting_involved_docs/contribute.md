## Contributing to mdmdirector

List of steps on how to make a contribution

- Review identified opportunities in the [To Do](to_do.md) list.

- File issues by navigating to the `Issues` tab and selecting the `New Issue` button. Bug reporting template is included from Github.

- Open Pull Requests by navigating to the `Pull Requests` tab and selecting the `changes from` and `changes to` branches and then click the `Create Pull Request` button. PR template is included from Github.

Once a change has been merged, follow steps to add to CHANGELOG.md

- give your change a tag (like a git commit message):
    (e.g.) git tag -a v0.5.2 -m "Releasing version v0.5.2"
- run git-chglog -o CHANGELOG.md

This accesses the installed go pkg [git-chglog](https://github.com/git-chglog/git-chglog#faq). Check out the link for additional information!
