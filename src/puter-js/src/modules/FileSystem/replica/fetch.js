/*
 * Copyright (C) 2024-present Puter Technologies Inc.
 *
 * This file is part of Puter.
 *
 * Puter is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published
 * by the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with this program.  If not, see <https://www.gnu.org/licenses/>.
 */

// NB: Avoid naming this function "fetch" to prevent shadowing the built-in fetch API in JavaScript.
import FSTree from './tree.js';

const replica_fetch = async function(path) {
    console.log('Fetching replica for path:', path);

    if (!path) {
        throw new Error('Path is required');
    }
    
    // If auth token is not provided and we are in the web environment, 
    // try to authenticate with Puter
    if(!puter.authToken && puter.env === 'web'){
        try{
            await puter.ui.authenticateWithPuter();
        }catch(e){
            // if authentication fails, throw an error
            throw new Error('Authentication failed.');
        }
    }

    console.log(`about to talk with url: ${puter.APIOrigin}/api/fs/replica/fetch`);
    
    const response = await fetch(`${puter.APIOrigin}/api/fs/replica/fetch`, {
        method: 'POST',
        headers: {
            'Authorization': `Bearer ${puter.authToken}`,
            'Content-Type': 'application/json'
        },
        body: JSON.stringify({ path })
    });

    if (!response.ok) {
        const errText = await response.text();
        throw new Error(`fetch failed ${response.status}: ${errText}`);
    }

    // Read ONCE
    const data = await response.json();
    console.log('replica fetch payload:', data);

    // initialize the FSTree
    window.FSTree = new FSTree(data);

    window.replica_available = true;
    
    // Update the replica status widget
    window.updateReplicaStatusWidget();

    return data;
};

export default replica_fetch;
