CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE jobs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    type VARCHAR(50) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    scheduled_at TIMESTAMPTZ,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 3,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

  CREATE TABLE job_results (                                                                                                  
      id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),                                                                         
      job_id UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,   
      job_payload JSONB,
      result_data JSONB NOT NULL,                                                                                            
      created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()                                                                           
  );
   
CREATE INDEX idx_job_results_job_id ON job_results(job_id);   

CREATE INDEX idx_jobs_status ON jobs(status);
CREATE INDEX idx_jobs_scheduled_at ON jobs(scheduled_at) WHERE scheduled_at IS NOT NULL;
CREATE INDEX idx_jobs_type ON jobs(type);

CREATE OR REPLACE FUNCTION notify_new_job() RETURNS trigger AS $$
  BEGIN
      PERFORM pg_notify(
          'new_job',
          json_build_object('id', NEW.id, 'job_type', NEW.type)::text
      );
      RETURN NEW;
  END;
  $$ LANGUAGE plpgsql;

CREATE TRIGGER job_inserted
  AFTER INSERT ON jobs
  FOR EACH ROW EXECUTE FUNCTION notify_new_job();