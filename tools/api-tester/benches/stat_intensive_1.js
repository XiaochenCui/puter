const chai = require('chai');
chai.use(require('chai-as-promised'))
const expect = chai.expect;

module.exports = {
    name: 'stat intensive 1',
    description: 'create 10 directories and 1000 subdirectories in each, then stat them over and over',
    do: async t => {
        console.log('stat intensive 1');

        const path = '/admin/Desktop/client_replica_poc_2';
        t.mkdir(path);
        t.cd(path);

        const dir_count = 10;
        const subdir_count = 10000;

        // key: uuid
        // value: path
        const dirs = {};

        for (let i = 0; i < dir_count; i++) {
            await t.mkdir(`dir_${i}`);
            for (let j = 0; j < subdir_count; j++) {
                const subdir = await t.mkdir(`dir_${i}/subdir_${j}`);
                dirs[subdir.uid] = subdir.path;

                // write 10 files in each subdir
                for (let k = 0; k < 10; k++) {
                    const content = `example ${i} ${j} ${k}`;
                    await t.write(`dir_${i}/subdir_${j}/file_${k}.txt`, content, { overwrite: true });
                }
            }
        }

        // exit
        process.exit(0);

        const start = Date.now();
        for (let i = 0; i < 10; i++) {
            for (const [uuid, path] of Object.entries(dirs)) {
                const stat = await t.stat_uuid(uuid);
                expect(stat.is_dir).equal(true);
                expect(stat.path).equal(path);
            }
        }
        const duration = Date.now() - start;
        return { duration };
    }
};