#! /usr/bin/env python3

import time

import cxc_toolkit

from tests.ci import common


def run_benchmark():
    # =========================================================================
    # clean ports
    # =========================================================================

    # clean port 4100 for backend server
    cxc_toolkit.exec.run_command("fuser -k 4100/tcp", ignore_failure=True)

    # =========================================================================
    # config server
    # =========================================================================
    cxc_toolkit.exec.run_command("npm install")
    common.init_backend_config()
    admin_password = common.get_admin_password()

    # =========================================================================
    # start backend server
    # =========================================================================
    cxc_toolkit.exec.run_background(
        "npm start", work_dir=common.PUTER_ROOT, log_path="/tmp/backend.log"
    )
    # wait 10s for the server to start
    time.sleep(10)


if __name__ == "__main__":
    run_benchmark()
