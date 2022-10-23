## Contributing to mdmdirector

List of steps on how to make a contribution

- File issues using the [bug](./ISSUE_TEMPLATE/bug.md) template.

- Open Pull Requests using the steps outlined below:

[Sample Pull Request](sample_pr.md)

Once a change has been merged, follow steps to add to CHANGELOG.md

- give your change a tag (like a git commit message):
    (e.g.) git tag -a v0.5.2 -m "Releasing version v0.5.2"
- run git-chglog -o CHANGELOG.md

This accesses the installed go pkg [git-chglog](https://github.com/git-chglog/git-chglog#faq). Check out the link for additional information!
