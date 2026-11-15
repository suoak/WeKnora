"""Route container termination through the server's existing interrupt cleanup."""

import signal


def wait_for_termination(server, logger):
    """Handle TERM as well as Ctrl-C, including when Python is container PID 1."""
    previous = signal.getsignal(signal.SIGTERM)

    def interrupt(_signum, _frame):
        raise KeyboardInterrupt

    signal.signal(signal.SIGTERM, interrupt)
    try:
        try:
            server.wait_for_termination()
        except KeyboardInterrupt:
            logger.info("Received termination signal, shutting down server")
            if not server.stop(0).wait(timeout=5):
                logger.warning("DocReader gRPC shutdown has not completed after five seconds")
    finally:
        signal.signal(signal.SIGTERM, previous)
