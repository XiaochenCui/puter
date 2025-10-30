#! /usr/bin/env python3

import time
import yaml

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
    # wait for the server to start
    time.sleep(5)

    # =========================================================================
    # generate test users
    # =========================================================================
    USERS_FILE = "./tests/benchmark/abc/config/users.yaml"
    users = []
    for i in range(1, 101):
        user_num = f"{i:04d}"
        user = {
            "username": f"bob_{user_num}",
            "password": f"password_{user_num}",
            "email": f"bob_{user_num}@puter.com",
        }
        users.append(user)

    with open(USERS_FILE, "w") as f:
        yaml.dump(users, f, default_flow_style=False)


if __name__ == "__main__":
    run_benchmark()
