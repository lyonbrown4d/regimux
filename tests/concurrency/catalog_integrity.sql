SELECT r.alias, r.name, r.pull_count, COALESCE(p.actual_count, 0) AS actual_count,
       r.policy_denied_pull_count, COALESCE(p.actual_denied, 0) AS actual_denied
FROM meta_repositories r
LEFT JOIN (
    SELECT alias, repository, SUM(count) AS actual_count, SUM(policy_denied_count) AS actual_denied
    FROM meta_pulls GROUP BY alias, repository
) p ON p.alias = r.alias AND p.repository = r.name
WHERE r.pull_count <> COALESCE(p.actual_count, 0)
   OR r.policy_denied_pull_count <> COALESCE(p.actual_denied, 0);

SELECT u.alias, u.pull_count, COALESCE(r.actual_count, 0) AS actual_count,
       u.policy_denied_pull_count, COALESCE(r.actual_denied, 0) AS actual_denied
FROM meta_upstreams u
LEFT JOIN (
    SELECT upstream_id, SUM(pull_count) AS actual_count, SUM(policy_denied_pull_count) AS actual_denied
    FROM meta_repositories GROUP BY upstream_id
) r ON r.upstream_id = u.id
WHERE u.pull_count <> COALESCE(r.actual_count, 0)
   OR u.policy_denied_pull_count <> COALESCE(r.actual_denied, 0);

SELECT p.alias, p.repository, SUM(p.count) AS actual_count,
       SUM(p.policy_denied_count) AS actual_denied
FROM meta_pulls p
LEFT JOIN meta_repositories r ON r.alias = p.alias AND r.name = p.repository
WHERE r.id IS NULL
GROUP BY p.alias, p.repository;

SELECT r.alias, r.name, r.upstream_id
FROM meta_repositories r
LEFT JOIN meta_upstreams u ON u.id = r.upstream_id
WHERE u.id IS NULL;
