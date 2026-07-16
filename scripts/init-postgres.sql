\set ON_ERROR_STOP on

SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', 'vessica_control_user', :'control_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'vessica_control_user')
\gexec
SELECT format('ALTER ROLE %I LOGIN PASSWORD %L', 'vessica_control_user', :'control_password')
\gexec

SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', 'vessica_knowledge_user', :'knowledge_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'vessica_knowledge_user')
\gexec
SELECT format('ALTER ROLE %I LOGIN PASSWORD %L', 'vessica_knowledge_user', :'knowledge_password')
\gexec

SELECT format('CREATE DATABASE %I OWNER %I', 'vessica_control', 'vessica_control_user')
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'vessica_control')
\gexec
ALTER DATABASE vessica_control OWNER TO vessica_control_user;
REVOKE CONNECT ON DATABASE vessica_control FROM PUBLIC;
GRANT CONNECT ON DATABASE vessica_control TO vessica_control_user;
GRANT CONNECT ON DATABASE vessica_control TO CURRENT_USER;

SELECT format('CREATE DATABASE %I OWNER %I', 'vessica_knowledge', 'vessica_knowledge_user')
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'vessica_knowledge')
\gexec
ALTER DATABASE vessica_knowledge OWNER TO vessica_knowledge_user;
REVOKE CONNECT ON DATABASE vessica_knowledge FROM PUBLIC;
GRANT CONNECT ON DATABASE vessica_knowledge TO vessica_knowledge_user;
GRANT CONNECT ON DATABASE vessica_knowledge TO CURRENT_USER;

\connect vessica_knowledge
CREATE EXTENSION IF NOT EXISTS vector;
