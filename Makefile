# Makefile — version bump & release tagging for the go-tools multi-module repo.
#
# This repo publishes one Go module per top-level directory, each tagged
# independently as `<module>/vX.Y.Z`, plus a root tag `vX.Y.Z`. A release bumps
# every module to the SAME version in lockstep.
#
# Usage:
#   make version                 # print the current and next versions
#   make patch                   # vX.Y.Z   -> vX.Y.(Z+1)
#   make minor                   # vX.Y.Z   -> vX.(Y+1).0
#   make major                   # vX.Y.Z   -> v(X+1).0.0
#   make push                    # push HEAD's branch + all tags to origin
#   make bump-patch / bump-minor / bump-major   # bump AND push in one step
#
# The bump targets create lightweight tags on the current HEAD (matching the
# repo's existing tagging style) for the root and every published module. They
# refuse to run on a dirty tree or if the target tags already exist.

SHELL := /bin/bash
.DEFAULT_GOAL := help

# Remote to push tags to.
REMOTE ?= origin

# Published modules: every directory containing a go.mod, excluding the repo
# root and the non-published `examples` module. Computed at parse time so new
# modules are picked up automatically.
MODULES := $(shell find . -name go.mod -mindepth 2 -maxdepth 2 \
	-not -path './examples/*' -print | sed 's|^\./||;s|/go.mod$$||' | sort)

# Latest root version tag (vX.Y.Z), or v0.0.0 when none exists yet.
CURRENT := $(shell git tag --list 'v[0-9]*.[0-9]*.[0-9]*' | sort -V | tail -1)
CURRENT := $(if $(CURRENT),$(CURRENT),v0.0.0)

# Numeric components of CURRENT (strip the leading 'v').
_VER   := $(patsubst v%,%,$(CURRENT))
MAJOR  := $(word 1,$(subst ., ,$(_VER)))
MINOR  := $(word 2,$(subst ., ,$(_VER)))
PATCH  := $(word 3,$(subst ., ,$(_VER)))

NEXT_MAJOR := v$(shell echo $$(($(MAJOR)+1))).0.0
NEXT_MINOR := v$(MAJOR).$(shell echo $$(($(MINOR)+1))).0
NEXT_PATCH := v$(MAJOR).$(MINOR).$(shell echo $$(($(PATCH)+1)))

.PHONY: help version modules major minor patch \
	bump-major bump-minor bump-patch push _check-clean

help: ## Show this help
	@echo "go-tools release targets:"
	@echo "  make version       Show current + next versions and module list"
	@echo "  make patch         Tag a patch release  ($(CURRENT) -> $(NEXT_PATCH))"
	@echo "  make minor         Tag a minor release  ($(CURRENT) -> $(NEXT_MINOR))"
	@echo "  make major         Tag a major release  ($(CURRENT) -> $(NEXT_MAJOR))"
	@echo "  make push          Push branch + all tags to '$(REMOTE)'"
	@echo "  make bump-patch    patch + push"
	@echo "  make bump-minor    minor + push"
	@echo "  make bump-major    major + push"

version: ## Print current/next versions
	@echo "current: $(CURRENT)"
	@echo "  patch: $(NEXT_PATCH)"
	@echo "  minor: $(NEXT_MINOR)"
	@echo "  major: $(NEXT_MAJOR)"

modules: ## List the modules that get tagged
	@echo "root + $(words $(MODULES)) modules:"
	@for m in $(MODULES); do echo "  $$m"; done

# Fail if the working tree (or index) has uncommitted changes — tags must point
# at a committed state.
_check-clean:
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "error: working tree is dirty; commit or stash before tagging." >&2; \
		git status --short >&2; \
		exit 1; \
	fi

# $(call do_tag,vX.Y.Z) — create the root tag and one tag per module on HEAD,
# after verifying none of them already exist. Lightweight tags, matching the
# repo's existing release style.
define do_tag
	@set -euo pipefail; \
	ver='$(1)'; \
	tags="$$ver"; \
	for m in $(MODULES); do tags="$$tags $$m/$$ver"; done; \
	for t in $$tags; do \
		if git rev-parse -q --verify "refs/tags/$$t" >/dev/null; then \
			echo "error: tag already exists: $$t" >&2; exit 1; \
		fi; \
	done; \
	head=$$(git rev-parse --short HEAD); \
	echo "Tagging $(CURRENT) -> $$ver at $$head (root + $(words $(MODULES)) modules)"; \
	for t in $$tags; do git tag "$$t" && echo "  + $$t"; done; \
	echo "Done. Review with 'git tag | grep $$ver', then 'make push'."
endef

# $(call push_release,vX.Y.Z) — push the current branch and exactly this
# version's tags (root + every module) to $(REMOTE). Pushing only the new tags
# avoids re-publishing unrelated local tags.
define push_release
	@set -euo pipefail; \
	ver='$(1)'; \
	branch=$$(git rev-parse --abbrev-ref HEAD); \
	tags="$$ver"; \
	for m in $(MODULES); do tags="$$tags $$m/$$ver"; done; \
	echo "Pushing $$branch + $$ver tags to $(REMOTE)"; \
	git push $(REMOTE) "$$branch"; \
	git push $(REMOTE) $$tags
endef

major: _check-clean ## Bump major (vX.Y.Z -> v(X+1).0.0) and tag
	$(call do_tag,$(NEXT_MAJOR))

minor: _check-clean ## Bump minor (vX.Y.Z -> v.(Y+1).0) and tag
	$(call do_tag,$(NEXT_MINOR))

patch: _check-clean ## Bump patch (vX.Y.Z -> vX.Y.(Z+1)) and tag
	$(call do_tag,$(NEXT_PATCH))

push: ## Push the current branch and ALL local tags to $(REMOTE)
	@branch=$$(git rev-parse --abbrev-ref HEAD); \
	echo "Pushing $$branch + all tags to $(REMOTE)"; \
	git push $(REMOTE) "$$branch"; \
	git push $(REMOTE) --tags

# bump-* do everything in one go: verify clean, tag root + all modules, then
# push the branch and exactly the new tags. Single recipe per target so tagging
# always happens before the push (no prerequisite ordering / -j races).
bump-major: _check-clean ## Bump major, tag, and push — all in one
	$(call do_tag,$(NEXT_MAJOR))
	$(call push_release,$(NEXT_MAJOR))

bump-minor: _check-clean ## Bump minor, tag, and push — all in one
	$(call do_tag,$(NEXT_MINOR))
	$(call push_release,$(NEXT_MINOR))

bump-patch: _check-clean ## Bump patch, tag, and push — all in one
	$(call do_tag,$(NEXT_PATCH))
	$(call push_release,$(NEXT_PATCH))
