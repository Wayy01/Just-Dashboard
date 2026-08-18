# Contributing

Thanks for looking. Issues and pull requests are welcome.

## Licence of the project

This project is licensed under the **GNU Affero General Public License v3.0**
(see [`LICENSE`](LICENSE)).

The Affero clause is deliberate. This dashboard is root-equivalent software, and
the licence means anyone who runs a modified version as a network service has to
publish their modifications. You are free to run it, change it and distribute
it; you are not free to take it closed.

## Licence of your contribution

By opening a pull request you agree to the following. There is nothing to sign —
submitting the PR is the agreement.

1. **You wrote it, or you have the right to submit it.** Your contribution is
   your own work, or you have permission from whoever owns it. If your employer
   has rights to work you do, you have their clearance to contribute it.

2. **Your contribution is licensed under AGPL-3.0**, the same terms as the rest
   of the project.

3. **You also grant the project owner (Wayy01) a separate, additional licence**
   to your contribution: perpetual, worldwide, non-exclusive, royalty-free and
   irrevocable, to use, reproduce, modify, distribute and sublicense it under
   *any* terms, including proprietary ones.

You keep the copyright to your work. Point 3 is an additional grant, not a
transfer — you can still do anything you like with your own code.

### Why point 3 exists

It is the only way the project can offer a commercial licence later without
having to track down every past contributor for permission. Practically, it
means paid multi-server features can fund the maintenance of the open source
part, rather than the project stalling the way most single-maintainer
infrastructure tools eventually do.

If that trade is not one you want to make, please open an issue describing the
change instead of a pull request — a good bug report is worth as much, and it
carries no licensing question at all.

## Before you open a pull request

- Run the checks: `cd frontend && bun run build` and `cd backend && go build ./...`.
- Keep the security posture intact. The network allowlist runs before
  authentication, two-factor is mandatory, and irreversible actions require a
  typed confirmation phrase enforced server-side. A change that weakens any of
  those needs to say so explicitly in the PR description.
- Match the surrounding code. Comments explain *why*, not *what*.

## Security issues

Please do not open a public issue for a vulnerability. Report it privately
through GitHub's **Security → Report a vulnerability** on this repository.
