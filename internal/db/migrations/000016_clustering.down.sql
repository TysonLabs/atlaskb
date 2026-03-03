-- PostgreSQL cannot remove enum values. Clean up data only.
DELETE FROM relationships WHERE kind = 'member_of';
DELETE FROM entities WHERE kind = 'cluster';
