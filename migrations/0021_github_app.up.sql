-- W2 full GitHub App: installation → customer mapping, replacing the bot's
-- hard-coded zero-UUID routing and global PAT. Keyed on GitHub's immutable
-- numeric ids (installation_id, repo_id) so a repo rename/transfer doesn't
-- orphan the mapping.
CREATE TABLE github_installation (
    installation_id BIGINT      PRIMARY KEY,            -- GitHub installation.id
    account_login   TEXT        NOT NULL,               -- org/user the App is installed on
    account_type    TEXT        NOT NULL DEFAULT 'Organization',
    customer_id     UUID        REFERENCES customers(id) ON DELETE CASCADE, -- NULL until connected
    suspended_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE github_repo (
    repo_id         BIGINT      PRIMARY KEY,            -- GitHub repository.id (stable across renames)
    installation_id BIGINT      NOT NULL REFERENCES github_installation(installation_id) ON DELETE CASCADE,
    repo_full_name  TEXT        NOT NULL,               -- "owner/name" (current; updated on rename)
    customer_id     UUID        REFERENCES customers(id) ON DELETE CASCADE, -- per-repo override of the installation default
    connected       BOOLEAN     NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_github_repo_fullname     ON github_repo(repo_full_name);
CREATE INDEX idx_github_repo_installation ON github_repo(installation_id);
CREATE INDEX idx_github_installation_customer ON github_installation(customer_id);
