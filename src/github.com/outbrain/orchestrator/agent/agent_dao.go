/*
   Copyright 2014 Outbrain Inc.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package agent

import (
	"fmt"
	"net/http"
	"io/ioutil"
	"errors"
	"encoding/json"
	"strings"
	"time"
				
	"github.com/outbrain/sqlutils"
	"github.com/outbrain/orchestrator/db"
	"github.com/outbrain/orchestrator/config"
	"github.com/outbrain/log"
)



func readResponse(res *http.Response, err error) ([]byte, error) {
	if err != nil { return nil, err }
	
	defer res.Body.Close()
    body, err := ioutil.ReadAll(res.Body)
	if err != nil { return nil, err }

	if res.Status == "500" { 
		return body, errors.New("Response Status 500")
	}
	
	return body, nil
}

// SubmitAgent submits a new agent for listing
func SubmitAgent(hostname string, port int, token string) (string, error) {
	db,	err	:=	db.OpenOrchestrator()
	if err != nil {return "", log.Errore(err)}
	
	_, err = sqlutils.Exec(db, `
			replace 
				into host_agent (
					hostname, port, token, last_submitted
				) VALUES (
					?, ?, ?, NOW()
				)
			`,
			hostname,
			port,
		 	token,
		 )
	if err != nil {return "", log.Errore(err)}
	
	return hostname, err
}



// ForgetLongUnseenInstances will remove entries of all instacnes that have long since been last seen.
func ForgetLongUnseenAgents() error {
	db,	err	:=	db.OpenOrchestrator()
	if err != nil {return log.Errore(err)}
	
	_, err = sqlutils.Exec(db, `
			delete 
				from host_agent 
			where 
				last_submitted < NOW() - interval ? hour`,
			config.Config.UnseenAgentForgetHours,
		 )
	return err		 
}


func ReadOutdatedAgentsHosts() ([]string, error) {
	res := []string{}
	query := fmt.Sprintf(`
		select 
			hostname 
		from 
			host_agent 
		where
			IFNULL(last_checked < now() - interval %d minute, true)
			`, 
    		config.Config.AgentPollMinutes)
	db,	err	:=	db.OpenOrchestrator()
    if err != nil {goto Cleanup}
    
    err = sqlutils.QueryRowsMap(db, query, func(m sqlutils.RowMap) error {
    	hostname := m.GetString("hostname")
    	res = append(res, hostname)
    	return nil       	
   	})
	Cleanup:

	if err	!=	nil	{
		log.Errore(err)
	}
	return res, err
}


// ReadAgents returns a list of all known agents
func ReadAgents() ([]Agent, error) {
	res := []Agent{}
	query := `
		select 
			hostname,
			port,
			token,
			last_submitted
		from 
			host_agent
		order by
			hostname
		`;
	db,	err	:=	db.OpenOrchestrator()
    if err != nil {goto Cleanup}
    
    err = sqlutils.QueryRowsMap(db, query, func(m sqlutils.RowMap) error {
    	agent := Agent{}
    	agent.Hostname = m.GetString("hostname")
    	agent.Port = m.GetInt("port")
    	agent.Token = "" 
    	agent.LastSubmitted = m.GetString("last_submitted") 

    	res = append(res, agent)
    	return nil       	
   	})
	Cleanup:

	if err != nil	{
		log.Errore(err)
	}
	return res, err

}


// ReadAgents returns a list of all known agents
func ReadCountMySQLSnapshots(hostnames []string) (map[string]int, error) {
	res := make(map[string]int)
	query := fmt.Sprintf(`
		select 
			hostname,
			count_mysql_snapshots
		from 
			host_agent
		where
			hostname in (%s)
		order by
			hostname
		`, sqlutils.InClauseStringValues(hostnames));
	db,	err	:=	db.OpenOrchestrator()
    if err != nil {goto Cleanup}
    
    err = sqlutils.QueryRowsMap(db, query, func(m sqlutils.RowMap) error {
    	res[m.GetString("hostname")] = m.GetInt("count_mysql_snapshots")
    	return nil       	
   	})
	Cleanup:

	if err != nil	{
		log.Errore(err)
	}
	return res, err
}


func readAgentBasicInfo(hostname string) (Agent, string, error) {
	agent := Agent{}
	token := ""
	query := fmt.Sprintf(`
		select 
			hostname,
			port,
			token,
			last_submitted
		from 
			host_agent
		where
			hostname = '%s'
		`, hostname);
	db,	err	:=	db.OpenOrchestrator()
    if err != nil {return agent, "", err}
    
    err = sqlutils.QueryRowsMap(db, query, func(m sqlutils.RowMap) error {
    	agent.Hostname = m.GetString("hostname")
    	agent.Port = m.GetInt("port")
    	agent.LastSubmitted = m.GetString("last_submitted")
    	token = m.GetString("token")
    	
    	return nil
   	})

	if token == "" {
		return agent, "", log.Errorf("Cannot get agent/token: %s", hostname) 
	}
	return agent, token, nil
}




// UpdateAgentLastChecked updates the last_check timestamp in the orchestrator backed database 
// for a given agent
func UpdateAgentLastChecked(hostname string) error {
	db,	err	:=	db.OpenOrchestrator()
	if err != nil {return log.Errore(err)}
	
	_, err = sqlutils.Exec(db, `
        	update 
        		host_agent 
        	set
        		last_checked = NOW()
			where 
				hostname = ?`,
		 	hostname,
		 	)
    if err != nil {return log.Errore(err)}

	return nil
}



// UpdateAgentLastChecked updates the last_check timestamp in the orchestrator backed database 
// for a given agent
func UpdateAgentInfo(hostname string, agent Agent) error {
	db,	err	:=	db.OpenOrchestrator()
	if err != nil {return log.Errore(err)}
	
	_, err = sqlutils.Exec(db, `
        	update 
        		host_agent 
        	set
        		last_seen = NOW(),
        		count_mysql_snapshots = ?
			where 
				hostname = ?`,
		 	len(agent.LogicalVolumes), 
		 	hostname,
		 	)
    if err != nil {return log.Errore(err)}

	return nil
}


// GetAgent gets a single agent status from the agent service
func GetAgent(hostname string) (Agent, error) {
	agent, token, err := readAgentBasicInfo(hostname)
    if err != nil {goto Cleanup}

	// All seems to be in order. Now make some inquiries from orchestrator-agent service:
	{
		uri := fmt.Sprintf("http://%s:%d/api", agent.Hostname, agent.Port)
		log.Debugf("orchestrator-agent uri: %s", uri)

		{		
			availableLocalSnapshotsUri := fmt.Sprintf("%s/available-snapshots-local?token=%s", uri, token)
			body, err := readResponse(http.Get(availableLocalSnapshotsUri))
			if err != nil {return agent, log.Errore(err)}
	
			err = json.Unmarshal(body, &agent.AvailableLocalSnapshots)
			if err != nil {return agent, log.Errore(err)}
		}
		{		
			availableSnapshotsUri := fmt.Sprintf("%s/available-snapshots?token=%s", uri, token)
			body, err := readResponse(http.Get(availableSnapshotsUri))
			if err != nil {return agent, log.Errore(err)}
	
			err = json.Unmarshal(body, &agent.AvailableSnapshots)
			if err != nil {return agent, log.Errore(err)}
		}
		{
			lvSnapshotsUri := fmt.Sprintf("%s/lvs-snapshots?token=%s", uri, token)		
			body, err := readResponse(http.Get(lvSnapshotsUri))
			if err == nil {
				err = json.Unmarshal(body, &agent.LogicalVolumes)
			}
			//if err != nil {return agent, log.Errore(err)}
		}
		{
			mountUri := fmt.Sprintf("%s/mount?token=%s", uri, token)		
			body, err := readResponse(http.Get(mountUri))
			if err == nil {
				err = json.Unmarshal(body, &agent.MountPoint)
			}
			//if err != nil {return agent, log.Errore(err)}
		}
		{
			mySQLRunningUri := fmt.Sprintf("%s/mysql-status?token=%s", uri, token)		
			body, err := readResponse(http.Get(mySQLRunningUri))
			if err == nil {
				err = json.Unmarshal(body, &agent.MySQLRunning)
			}
			//if err != nil {return agent, log.Errore(err)}
		}
		{
			mySQLRunningUri := fmt.Sprintf("%s/mysql-port?token=%s", uri, token)		
			body, err := readResponse(http.Get(mySQLRunningUri))
			if err == nil {
				err = json.Unmarshal(body, &agent.MySQLPort)
			}
		}
		{
			mySQLDiskUsageUri := fmt.Sprintf("%s/mysql-du?token=%s", uri, token)		
			body, err := readResponse(http.Get(mySQLDiskUsageUri))
			if err == nil {
				err = json.Unmarshal(body, &agent.MySQLDiskUsage)
			}
			//if err != nil {return agent, log.Errore(err)}
		}
	}
	Cleanup:
	if err != nil{
		return agent, log.Errore(err)
	}
	return agent, err
}


func executeAgentCommand(hostname string, command string) (Agent, error) {
	agent, token, err := readAgentBasicInfo(hostname)
    if err != nil {return agent, err}

	// All seems to be in order. Now make some inquiries from orchestrator-agent service:
	uri := fmt.Sprintf("http://%s:%d/api", agent.Hostname, agent.Port)

	var fullCommand string
	if strings.Contains(command, "?") {
		fullCommand = fmt.Sprintf("%s&token=%s", command, token);
	} else {
		fullCommand = fmt.Sprintf("%s?token=%s", command, token);
	}
	log.Debugf("orchestrator-agent command: %s", fullCommand)
	agentCommandUri := fmt.Sprintf("%s/%s", uri, fullCommand)
	_, err = readResponse(http.Get(agentCommandUri))
	if err != nil {return agent, log.Errore(err)}

	return agent, err	
}


// Unmount unmounts the designated snapshot mount point
func Unmount(hostname string) (Agent, error) {
	return executeAgentCommand(hostname, "umount")
}


// MountLV
func MountLV(hostname string, lv string) (Agent, error) {
	return executeAgentCommand(hostname, fmt.Sprintf("mountlv?lv=%s", lv))
}



// MySQLStop
func deleteMySQLDatadir(hostname string) (Agent, error) {
	return executeAgentCommand(hostname, "delete-mysql-datadir")
}


// MySQLStop
func MySQLStop(hostname string) (Agent, error) {
	return executeAgentCommand(hostname, "mysql-stop")
}



// MySQLStart
func MySQLStart(hostname string) (Agent, error) {
	return executeAgentCommand(hostname, "mysql-start")
}



func ReceiveMySQLSeedData(hostname string) (Agent, error) {
	return executeAgentCommand(hostname, "receive-mysql-seed-data")
}


func SendMySQLSeedData(hostname string, targetHostname string) (Agent, error) {
	return executeAgentCommand(hostname, fmt.Sprintf("send-mysql-seed-data/%s", targetHostname))
}



// SubmitSeedEntry submits a new seed operation entry, returning its unique ID
func SubmitSeedEntry(targetHostname string, sourceHostname string) (int64, error) {
	db,	err	:=	db.OpenOrchestrator()
	if err != nil {return 0, log.Errore(err)}
	
	res, err := sqlutils.Exec(db, `
			insert 
				into agent_seed (
					target_hostname, source_hostname, start_timestamp
				) VALUES (
					?, ?, NOW()
				)
			`,
			targetHostname,
			sourceHostname,
		 )
	if err != nil {return 0, log.Errore(err)}
	id, err := res.LastInsertId()
	
	return id, err
}



// updateSeedComplete updates the seed entry, signing for completion
func updateSeedComplete(seedId int64, seedError error) error {
	db,	err	:=	db.OpenOrchestrator()
	if err != nil {return log.Errore(err)}
	
	_, err = sqlutils.Exec(db, `
			update 
				agent_seed
					set end_timestamp = NOW(),
					is_complete = 1,
					is_successful = ?
				where
					agent_seed_id = ?
			`,
			(seedError == nil),
			seedId,
		 )
	if err != nil {return log.Errore(err)}
	
	return nil
}


// submitSeedStateEntry submits a seed state: a single step in the overall seed process
func submitSeedStateEntry(seedId int64, action string, errorMessage string) (int64, error) {
	db,	err	:=	db.OpenOrchestrator()
	if err != nil {return 0, log.Errore(err)}
	
	res, err := sqlutils.Exec(db, `
			insert 
				into agent_seed_state (
					agent_seed_id, state_timestamp, state_action, error_message
				) VALUES (
					?, NOW(), ?, ?
				)
			`,
			seedId,
			action,
			errorMessage,
		 )
	if err != nil {return 0, log.Errore(err)}
	id, err := res.LastInsertId()
	
	return id, err
}


// updateSeedStateEntry updates seed step state
func updateSeedStateEntry(seedStateId int64, reason error) error {
	db,	err	:=	db.OpenOrchestrator()
	if err != nil {return log.Errore(err)}
	
	_, err = sqlutils.Exec(db, `
			update 
				agent_seed_state
					set error_message = ?
				where
					agent_seed_state_id = ?
			`,
			reason.Error(),
			seedStateId,
		 )
	if err != nil {return log.Errore(err)}
	
	return reason
}


// executeSeed
func executeSeed(seedId int64, targetHostname string, sourceHostname string) error {

	var err error
	var seedStateId int64
		
	seedStateId, _ = submitSeedStateEntry(seedId, fmt.Sprintf("getting target agent info for %s", targetHostname), "")
	targetAgent, err := GetAgent(targetHostname)
	if err != nil {return updateSeedStateEntry(seedStateId, err)}
	
	seedStateId, _ = submitSeedStateEntry(seedId, fmt.Sprintf("getting source agent info for %s", sourceHostname), "")
	sourceAgent, err := GetAgent(sourceHostname)
	if err != nil {return updateSeedStateEntry(seedStateId, err)}
	
	seedStateId, _ = submitSeedStateEntry(seedId, fmt.Sprintf("Checking MySQL status on target %s", targetHostname), "")
	if targetAgent.MySQLRunning {
		return updateSeedStateEntry(seedStateId, errors.New("MySQL is running on target host. Cowardly refusing to proceeed. Please stop the MySQL service"))
	}
	
	seedStateId, _ = submitSeedStateEntry(seedId, fmt.Sprintf("Looking up available snapshots on source %s", sourceHostname), "")
	if len(sourceAgent.LogicalVolumes) == 0 {
		return updateSeedStateEntry(seedStateId, errors.New("No logical volumes found on source host"))
	}
	
	seedStateId, _ = submitSeedStateEntry(seedId, fmt.Sprintf("Checking mount point on source %s", sourceHostname), "")
	if sourceAgent.MountPoint.IsMounted {
		return updateSeedStateEntry(seedStateId, errors.New("Volume already mounted on source host; please unmount"))
	}
	
	seedFromLogicalVolume := sourceAgent.LogicalVolumes[0]
	seedStateId, _ = submitSeedStateEntry(seedId, fmt.Sprintf("Mounting logical volume: %s", seedFromLogicalVolume.Path), "")
	_, err = MountLV(sourceHostname, seedFromLogicalVolume.Path)
	if err != nil {
		return updateSeedStateEntry(seedStateId, err)
	}
	sourceAgent, err = GetAgent(sourceHostname)
	seedStateId, _ = submitSeedStateEntry(seedId, fmt.Sprintf("MySQL data volume on source host %s is %d bytes", sourceHostname, sourceAgent.MountPoint.MySQLDiskUsage), "")
	
	seedStateId, _ = submitSeedStateEntry(seedId, fmt.Sprintf("Erasing MySQL data on %s", targetHostname), "")
	_, err = deleteMySQLDatadir(targetHostname)
	if err != nil {return updateSeedStateEntry(seedStateId, err)}
	
	// ...
	seedStateId, _ = submitSeedStateEntry(seedId, fmt.Sprintf("%s will now receive data in background", targetHostname), "")
	ReceiveMySQLSeedData(targetHostname)

	seedStateId, _ = submitSeedStateEntry(seedId, fmt.Sprintf("Waiting some time for %s to start listening for incoming data", targetHostname), "")
	time.Sleep(2 * time.Second)	
	
	seedStateId, _ = submitSeedStateEntry(seedId, fmt.Sprintf("%s will now send data to %s in background", sourceHostname, targetHostname), "")
	SendMySQLSeedData(sourceHostname, targetHostname)

	copyComplete := false
	numStaleIterations := 0
	var bytesCopied int64 = 0
	
	for !copyComplete {
		seedStateId, _ = submitSeedStateEntry(seedId, fmt.Sprintf("polling target agent info for %s", targetHostname), "")
		targetAgentPoll, err := GetAgent(targetHostname)
		if err != nil {return updateSeedStateEntry(seedStateId, err)}

		if targetAgentPoll.MySQLDiskUsage == bytesCopied {
			numStaleIterations++
		}
		if numStaleIterations > 10 {
			return updateSeedStateEntry(seedStateId, errors.New("10 iterations have passed without progress. Bailing out."))
		}
		bytesCopied = targetAgentPoll.MySQLDiskUsage
		
		if bytesCopied >= sourceAgent.MountPoint.MySQLDiskUsage {
			copyComplete = true
		} 
		copyPct := 100*bytesCopied/sourceAgent.MountPoint.MySQLDiskUsage
		seedStateId, _ = submitSeedStateEntry(seedId, fmt.Sprintf("Copied %d/%d bytes (%d%%)", bytesCopied, sourceAgent.MountPoint.MySQLDiskUsage, copyPct), "")
		
		if !copyComplete {
			time.Sleep(30 * time.Second)	
		}	
	}
	
	// Cleanup:
	
	seedStateId, _ = submitSeedStateEntry(seedId, fmt.Sprintf("Unmounting logical volume: %s", seedFromLogicalVolume.Path), "")
	_, err = Unmount(sourceHostname)
	if err != nil {
		return updateSeedStateEntry(seedStateId, err)
	}
	
	seedStateId, _ = submitSeedStateEntry(seedId, fmt.Sprintf("Starting MySQL on target: %s", targetHostname), "")
	_, err = MySQLStart(targetHostname)
	if err != nil {
		return updateSeedStateEntry(seedStateId, err)
	}
	
	
	return nil
}



// Seed
func Seed(targetHostname string, sourceHostname string) (int64, *Agent, error) {
	if targetHostname == sourceHostname {
		return 0, nil, log.Errorf("Cannot seed %s onto itself", targetHostname)
	}
	seedId, err := SubmitSeedEntry(targetHostname, sourceHostname)
	if err != nil {return 0, nil, log.Errore(err)}
	
	go func() {
		err := executeSeed(seedId, targetHostname, sourceHostname)
		updateSeedComplete(seedId, err)
	}()

	targetAgent, err := GetAgent(targetHostname)
	if err != nil {return 0, nil, log.Errore(err)}
	
	return seedId, &targetAgent, nil
}
