package state

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const postgresHumanTaskNotificationChannel = "windforce_human_task"

func (s *PostgresStore) SubscribeHumanTaskChanges(taskID string) (<-chan struct{}, func()) {
	s.humanTaskListen.Do(func() {
		go s.runHumanTaskNotificationListener(s.listenerContext)
	})
	return s.humanTaskSignals.subscribe(taskID)
}

func (s *PostgresStore) runHumanTaskNotificationListener(ctx context.Context) {
	backoff := 100 * time.Millisecond
	for ctx.Err() == nil {
		connection, err := pgx.Connect(ctx, s.databaseURL)
		if err == nil {
			_, err = connection.Exec(ctx, `LISTEN `+postgresHumanTaskNotificationChannel)
		}
		if err == nil {
			s.listenerReadyOnce.Do(func() { close(s.listenerReady) })
			backoff = 100 * time.Millisecond
			for ctx.Err() == nil {
				notification, waitErr := connection.WaitForNotification(ctx)
				if waitErr != nil {
					err = waitErr
					break
				}
				if notification.Channel == postgresHumanTaskNotificationChannel {
					s.humanTaskSignals.notify(strings.TrimSpace(notification.Payload))
				}
			}
		}
		if connection != nil {
			closeContext, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = connection.Close(closeContext)
			cancel()
		}
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if backoff < 5*time.Second {
			backoff *= 2
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
		}
	}
}

func notifyHumanTaskChangePostgresTx(ctx context.Context, tx pgx.Tx, taskID string) error {
	_, err := tx.Exec(ctx, `SELECT pg_notify($1, $2)`, postgresHumanTaskNotificationChannel, strings.TrimSpace(taskID))
	return err
}
