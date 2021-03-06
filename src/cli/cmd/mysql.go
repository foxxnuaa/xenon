/*
 * Xenon
 *
 * Copyright 2018 The Xenon Authors.
 * Code is licensed under the GPLv3.
 *
 */

package cmd

import (
	"cli/callx"
	"encoding/json"
	"fmt"
	"model"
	"time"
	"xbase/common"

	"github.com/spf13/cobra"
)

func NewMysqlCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mysql <subcommand>",
		Short: "mysql related commands",
	}

	cmd.AddCommand(NewMysqlStartMonitorCommand())
	cmd.AddCommand(NewMysqlStopMonitorCommand())
	cmd.AddCommand(NewMysqlStartCommand())
	cmd.AddCommand(NewMysqlShutDownCommand())
	cmd.AddCommand(NewMysqlRebuildMeCommand())
	cmd.AddCommand(NewMysqlDoBackupCommand())
	cmd.AddCommand(NewMysqlCancelBackupCommand())
	cmd.AddCommand(NewMysqlCreateUserCommand())
	cmd.AddCommand(NewMysqlCreateSuperUserCommand())
	cmd.AddCommand(NewMysqlDropUserCommand())
	cmd.AddCommand(NewMysqlChangePasswordCommand())
	cmd.AddCommand(NewMysqlSetVarCommand())
	cmd.AddCommand(NewMysqlKillCommand())
	cmd.AddCommand(NewMysqlStatusCommand())
	cmd.AddCommand(NewMysqlCreateUserWithPrivilegesCommand())
	cmd.AddCommand(NewMysqlGetUserCommand())

	return cmd
}

// stop monitor
func NewMysqlStopMonitorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stopmonitor",
		Short: "stop mysqld monitor",
		Run:   mysqlStopMonitorCommandFn,
	}

	return cmd
}

func mysqlStopMonitorCommandFn(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		ErrorOK(fmt.Errorf("too.many.args"))
	}
	log.Warning("prepare.to.stop.mysql.monitor")
	conf, err := GetConfig()
	ErrorOK(err)
	rsp, err := callx.StopMonitorRPC(conf.Server.Endpoint)
	ErrorOK(err)
	RspOK(rsp.RetCode)
	log.Warning("prepare.to.stop.mysql.monitor.done")
}

// start monitor
func NewMysqlStartMonitorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "startmonitor",
		Short: "start mysqld monitor",
		Run:   mysqlStartMonitorCommandFn,
	}

	return cmd
}

func mysqlStartMonitorCommandFn(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		ErrorOK(fmt.Errorf("too.many.args"))
	}
	log.Warning("prepare.to.start.mysql.monitor")

	conf, err := GetConfig()
	ErrorOK(err)
	rsp, err := callx.StartMonitorRPC(conf.Server.Endpoint)
	ErrorOK(err)
	RspOK(rsp.RetCode)
	log.Warning("prepare.to.start.mysql.monitor.done")
}

// start mysql
func NewMysqlStartCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "start mysql",
		Run:   mysqlStartCommandFn,
	}

	return cmd
}

func mysqlStartCommandFn(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		ErrorOK(fmt.Errorf("too.many.args"))
	}

	log.Warning("prepare.to.start.mysql")
	conf, err := GetConfig()
	ErrorOK(err)
	callx.StartMysqldRPC(conf.Server.Endpoint)
	log.Warning("start.mysql.done")
}

// shutdown mysql
func NewMysqlShutDownCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "shutdown",
		Run: mysqlShutDownCommandFn,
	}

	return cmd
}

func mysqlShutDownCommandFn(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		ErrorOK(fmt.Errorf("too.many.args"))
	}

	log.Warning("prepare.to.shutdown.mysql")

	conf, err := GetConfig()
	ErrorOK(err)

	// shutdown
	callx.ShutdownMysqldRPC(conf.Server.Endpoint)

	// wait mysqld shutdown
	callx.WaitMysqldShutdownRPC(conf.Server.Endpoint)
	log.Warning("shutdown.mysql.done")
}

// rebuild me
var (
	fromStr string
)

func NewMysqlRebuildMeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rebuildme [--from=endpoint]",
		Short: "rebuild a slave --from=endpoint",
		Run:   mysqlRebuildMeCommandFn,
	}
	cmd.Flags().StringVar(&fromStr, "from", "", "--from=endpoint")

	return cmd
}

func mysqlRebuildMeCommandFn(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		ErrorOK(fmt.Errorf("too.many.args"))
	}

	log.Warning(`=====prepare.to.rebuildme=====
			IMPORTANT: Please check that the backup run completes successfully.
			           At the end of a successful backup run innobackupex
			           prints "completed OK!".
			`)

	conf, err := GetConfig()
	ErrorOK(err)

	self := conf.Server.Endpoint
	bestone := ""

	// 1. first to check I am leader or not
	{
		log.Warning("S1-->check.raft.leader")
		leader, err := callx.GetClusterLeader(self)
		ErrorOK(err)
		if leader == self {
			log.Panic("[%v].I.am.leader.you.cant.rebuildme.sir", self)
		}
	}

	// 2. find the best to backup
	{
		if fromStr != "" {
			bestone = fromStr
		} else {
			bestone, err = callx.FindBestoneForBackup(self)
			ErrorOK(err)
		}
		log.Warning("S2-->prepare.rebuild.from[%v]....", bestone)
	}

	// 3. check bestone is not in BACKUPING
	{
		rsp, err := callx.GetMysqldStatusRPC(bestone)
		ErrorOK(err)
		if rsp.BackupStatus == model.MYSQLD_BACKUPING {
			log.Warning("S3-->check.bestone[%v].is.backuping....", bestone)
			log.Panic("bestone[%v].is.backuping....", bestone)
		}
		log.Warning("S3-->check.bestone[%v].is.OK....", bestone)
	}

	// 4. disable raft
	{
		log.Warning("S4-->disable.raft")
		if _, err := callx.DisableRaftRPC(self); err != nil {
			log.Error("disableRaftRPC.error[%v]", err)
		}
	}

	// 5. stop monitor
	{
		log.Warning("S5-->stop.monitor")
		callx.StopMonitorRPC(self)
	}

	// 6. force kill mysqld
	{
		log.Warning("S6-->kill.mysql")
		err := callx.KillMysqldRPC(self)
		ErrorOK(err)

		// wait
		err = callx.WaitMysqldShutdownRPC(self)
		ErrorOK(err)
	}

	// 7. check bestone is not in BACKUPING again
	{
		rsp, err := callx.GetMysqldStatusRPC(bestone)
		ErrorOK(err)
		if rsp.BackupStatus == model.MYSQLD_BACKUPING {
			log.Warning("S7-->check.bestone[%v].is.backuping....", bestone)
			log.Panic("bestone[%v].is.backuping....", bestone)
		}
		log.Warning("S7-->check.bestone[%v].is.OK....", bestone)
	}

	// 8. remove data files
	{
		datadir := conf.Backup.BackupDir
		cmds := "bash"
		args := []string{
			"-c",
			fmt.Sprintf("rm -rf %s/*", datadir),
		}

		_, err := common.RunCommand(cmds, args...)
		ErrorOK(err)
		log.Warning("S8-->rm.datadir[%v]", datadir)
	}

	// 9. do backup from bestone
	{
		log.Warning("S9-->xtrabackup.begin....")
		rsp, err := callx.RequestBackupRPC(bestone, conf, conf.Backup.BackupDir)
		ErrorOK(err)
		RspOK(rsp.RetCode)
		log.Warning("S9-->xtrabackup.end....")
	}

	// 10. do apply-log
	{
		log.Warning("S10-->apply-log.begin....")
		err := callx.DoApplyLogRPC(conf.Server.Endpoint, conf.Backup.BackupDir)
		ErrorOK(err)
		log.Warning("S10-->apply-log.end....")
	}

	// 11. start mysqld
	{
		log.Warning("S11-->start.mysql.begin...")
		if _, err := callx.StartMonitorRPC(self); err != nil {
			log.Error("start.mysql..error[%v]", err)
		}
		log.Warning("S11-->start.mysql.end...")
	}

	// 12. wait mysqld running
	{
		log.Warning("S12-->wait.mysqld.running.begin....")
		callx.WaitMysqldRunningRPC(self)
		log.Warning("S12-->wait.mysqld.running.end....")
	}

	// 13. wait mysql working
	{
		log.Warning("S13-->wait.mysql.working.begin....")
		callx.WaitMysqlWorkingRPC(self)
		log.Warning("S13-->wait.mysql.working.end....")
	}

	// 14. stop slave and reset slave all
	{
		log.Warning("S14-->stop.and.reset.slave.begin....")
		if _, err := callx.MysqlResetSlaveAllRPC(self); err != nil {
			log.Error("mysql.stop.adn.reset.slave.error[%v]", err)
		}
		log.Warning("S14-->stop.and.reset.slave.end....")
	}

	// 15. set gtid_purged
	{

		log.Warning("S15-->reset.master.begin....")
		callx.MysqlResetMasterRPC(self)
		log.Warning("S15-->reset.master.end....")

		gtid, err := callx.GetXtrabackupGTIDPurged(self, conf.Backup.BackupDir)
		ErrorOK(err)

		log.Warning("S15-->set.gtid_purged[%v].begin....", gtid)
		rsp, err := callx.SetGlobalVarRPC(self, fmt.Sprintf("SET GLOBAL gtid_purged='%s'", gtid))
		ErrorOK(err)
		RspOK(rsp.RetCode)
		log.Warning("S15-->set.gtid_purged.end....")
	}

	// 16. enable raft
	{
		log.Warning("S16-->enable.raft.begin...")
		if _, err := callx.EnableRaftRPC(self); err != nil {
			log.Error("enbleRaftRPC.error[%v]", err)
		}
		log.Warning("S16-->enable.raft.done...")
	}

	// 17. wait change to master
	{
		log.Warning("S17-->wait[%v ms].change.to.master...", conf.Raft.ElectionTimeout)
		time.Sleep(time.Duration(conf.Raft.ElectionTimeout))
	}

	// 18. start slave
	{
		log.Warning("S18-->start.slave.begin....")
		if _, err := callx.MysqlStartSlaveRPC(self); err != nil {
			log.Error("mysql.start.slave.error[%v]", err)
		} else {
			log.Warning("S18-->start.slave.end....")
		}
	}

	log.Warning("completed OK!")
	log.Warning("rebuildme.all.done....")
}

var (
	toStr string
)

func NewMysqlDoBackupCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup --to=backupdir",
		Short: "backup this mysql to backupdir",
		Run:   mysqlDoBackupCommandFn,
	}
	cmd.Flags().StringVar(&toStr, "to", "", "--to=backupdir")

	return cmd
}

func mysqlDoBackupCommandFn(cmd *cobra.Command, args []string) {
	if len(args) != 1 {
		cmd.Usage()
		ErrorOK(fmt.Errorf("args.must.be: --to=backupdir"))
	}

	conf, err := GetConfig()
	ErrorOK(err)

	self := conf.Server.Endpoint
	bestone := ""

	// 1. find the best to backup
	{
		bestone, err := callx.FindBestoneForBackup(self)
		ErrorOK(err)
		log.Warning("S1-->found.the.best.backup.host[%v]....", bestone)
	}

	backupdir := args[0]
	// 2. remove backupdir files
	{
		cmds := "bash"
		args := []string{
			"-c",
			fmt.Sprintf("rm -rf %s/*", backupdir),
		}

		_, err := common.RunCommand(cmds, args...)
		ErrorOK(err)
		log.Warning("S2-->rm.backupdir[%v]", backupdir)
	}

	// 3. do backup from bestone
	{
		log.Warning("S3-->xtrabackup.begin....")
		rsp, err := callx.RequestBackupRPC(bestone, conf, backupdir)
		ErrorOK(err)
		RspOK(rsp.RetCode)
		log.Warning("S3-->xtrabackup.end....")
	}

	// 4. do apply-log
	{
		log.Warning("S4-->apply-log.begin....")
		err := callx.DoApplyLogRPC(conf.Server.Endpoint, backupdir)
		ErrorOK(err)
		log.Warning("S4-->apply-log.end....")
	}

	log.Warning("completed OK!")
	log.Warning("backup.all.done....")
}

func NewMysqlCancelBackupCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "cancelbackup",
		Run: mysqlCancelBackupCommandFn,
	}

	return cmd
}

func mysqlCancelBackupCommandFn(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		cmd.Usage()
		ErrorOK(fmt.Errorf("too.many.args"))
	}

	conf, err := GetConfig()
	ErrorOK(err)

	self := conf.Server.Endpoint
	{
		log.Warning("backup.cancel.begin....")
		rsp, err := callx.BackupCancelRPC(self)
		ErrorOK(err)
		RspOK(rsp.RetCode)
		log.Warning("backup.cancel.done....")
	}
}

// create normal user
func NewMysqlCreateUserCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "createuser <user> <password>",
		Short: "create mysql normal user",
		Run:   mysqlCreateUserCommandFn,
	}

	return cmd
}

func mysqlCreateUserCommandFn(cmd *cobra.Command, args []string) {
	if len(args) != 2 {
		ErrorOK(fmt.Errorf("args.count.error:should.be.2"))
	}

	user := args[0]
	passwd := args[1]
	log.Warning("prepare.to.create.normaluser[%v]", user)
	conf, err := GetConfig()
	ErrorOK(err)

	self := conf.Server.Endpoint
	{
		leader, err := callx.GetClusterLeader(self)
		ErrorOK(err)
		rsp, err := callx.CreateNormalUserRPC(leader, user, passwd)
		ErrorOK(err)
		RspOK(rsp.RetCode)
	}
	log.Warning("create.normaluser[%v].done", user)
}

// create super user
func NewMysqlCreateSuperUserCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "createsuperuser <user> <password>",
		Short: "create mysql super user",
		Run:   mysqlCreateSuperUserCommandFn,
	}
	return cmd
}

func mysqlCreateSuperUserCommandFn(cmd *cobra.Command, args []string) {
	if len(args) != 2 {
		ErrorOK(fmt.Errorf("args.count.error:should.be.2"))
	}

	user := args[0]
	passwd := args[1]
	log.Warning("prepare.to.create.super.user[%v]", user)
	conf, err := GetConfig()
	ErrorOK(err)

	self := conf.Server.Endpoint
	{
		leader, err := callx.GetClusterLeader(self)
		ErrorOK(err)
		rsp, err := callx.CreateSuperUserRPC(leader, user, passwd)
		ErrorOK(err)
		RspOK(rsp.RetCode)
	}
	log.Warning("create.super.user[%v].done", user)
}

// drop user(normal&super)
func NewMysqlDropUserCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dropuser <user> <host>",
		Short: "drop mysql normal user",
		Run:   mysqlDropUserCommandFn,
	}

	return cmd
}

func mysqlDropUserCommandFn(cmd *cobra.Command, args []string) {
	if len(args) != 2 {
		ErrorOK(fmt.Errorf("args.count.error:should.be.2"))
	}

	user := args[0]
	host := args[1]
	log.Warning("prepare.to.drop.normaluser[%v]@[%v]", user, host)
	conf, err := GetConfig()
	ErrorOK(err)

	self := conf.Server.Endpoint
	{
		leader, err := callx.GetClusterLeader(self)
		ErrorOK(err)
		rsp, err := callx.DropUserRPC(leader, user, host)
		ErrorOK(err)
		RspOK(rsp.RetCode)
	}
	log.Warning("drop.normaluser[%v]@[%v].done", user, host)
}

// change normal user password
func NewMysqlChangePasswordCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "changepassword <user> <password>",
		Short: "update mysql normal user password",
		Run:   mysqlChangePasswordCommandFn,
	}

	return cmd
}

func mysqlChangePasswordCommandFn(cmd *cobra.Command, args []string) {
	if len(args) != 2 {
		ErrorOK(fmt.Errorf("args.count.error:should.be.2"))
	}

	user := args[0]
	passwd := args[1]
	log.Warning("prepare.to.changepassword.normaluser[%v]", user)
	conf, err := GetConfig()
	ErrorOK(err)

	self := conf.Server.Endpoint
	{
		leader, err := callx.GetClusterLeader(self)
		ErrorOK(err)

		rsp, err := callx.ChangeUserPasswordRPC(leader, user, passwd)
		ErrorOK(err)
		RspOK(rsp.RetCode)
	}
	log.Warning("create.changepassword[%v].done", user)
}

// set global sysvar
func NewMysqlSetVarCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sysvar",
		Short: "set global variables",
		Run:   mysqlSetGlobalVarCommandFn,
	}

	return cmd
}

func mysqlSetGlobalVarCommandFn(cmd *cobra.Command, args []string) {
	if len(args) != 1 {
		ErrorOK(fmt.Errorf("args.should.be.1"))
	}
	log.Warning("prepare.to.set.global.var[%v]", args[0])
	conf, err := GetConfig()
	ErrorOK(err)
	rsp, err := callx.SetGlobalVarRPC(conf.Server.Endpoint, args[0])
	ErrorOK(err)
	RspOK(rsp.RetCode)
	log.Warning("set.global.var[%v].done", args[0])
}

// kill mysql
func NewMysqlKillCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kill",
		Short: "kill mysql pid(becareful!)",
		Run:   mysqlKillCommandFn,
	}

	return cmd
}

func mysqlKillCommandFn(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		ErrorOK(fmt.Errorf("too.many.args"))
	}
	log.Warning("prepare.to.kill.mysql")
	conf, err := GetConfig()
	ErrorOK(err)
	err = callx.KillMysqldRPC(conf.Server.Endpoint)
	ErrorOK(err)
	log.Warning("prepare.to.kill.mysql.done")
}

// mysql status api format in JSON
func NewMysqlStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "mysql status in JSON(mysqld/slave_SQL/IO is running)",
		Run:   mysqlStatusCommandFn,
	}

	return cmd
}

func mysqlStatusCommandFn(cmd *cobra.Command, args []string) {
	type Status struct {
		Slave_io_running      bool   `json:"slave_io_running"`
		Slave_sql_running     bool   `json:"slave_sql_running"`
		Mysqldrunning         bool   `json:"mysqld_running"`
		Mysqlworking          bool   `json:"mysql_working"`
		Seconds_behind_master string `json:"seconds_behind_master"`
		Last_error            string `json:"last_error"`
		Monitor               string `json:"monitor"`
	}
	status := &Status{}

	if len(args) != 0 {
		ErrorOK(fmt.Errorf("too.many.args"))
	}
	conf, err := GetConfig()
	self := conf.Server.Endpoint
	ErrorOK(err)
	// mysqld info
	if running, err := callx.MysqldIsRunningRPC(self); err == nil {
		status.Mysqldrunning = running
		// slave info
		if running {
			rsp, err := callx.GetGTIDRPC(self)
			ErrorOK(err)

			status.Slave_io_running = rsp.GTID.Slave_IO_Running
			status.Slave_sql_running = rsp.GTID.Slave_SQL_Running
			status.Seconds_behind_master = rsp.GTID.Seconds_Behind_Master
			status.Last_error = rsp.GTID.Last_Error

			mysqlworking, err := callx.MysqlIsWorkingRPC(self)
			ErrorOK(err)
			status.Mysqlworking = mysqlworking
		}
	}

	if rsp, err := callx.GetMysqldStatusRPC(self); err == nil {
		status.Monitor = rsp.MonitorInfo
	}

	statusB, _ := json.Marshal(status)
	fmt.Printf("%s", string(statusB))
}

var (
	grantUser     string
	grantPasswd   string
	grantDatabase string
	grantTable    string
	grantHost     string
	grantPrivs    string
	requireSSL    string
)

// create normal user with privileges
func NewMysqlCreateUserWithPrivilegesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "createuserwithgrants",
		Short: "create mysql normal user with privileges",
		Run:   mysqlCreateUserWithPrivilegesCommandFn,
	}
	cmd.Flags().StringVar(&grantUser, "user", "", "--user=<user>")
	cmd.Flags().StringVar(&grantPasswd, "passwd", "", "--passwd=<passwd>")
	cmd.Flags().StringVar(&grantDatabase, "database", "", "--database=<database>")
	cmd.Flags().StringVar(&grantTable, "table", "", "--table=<table>")
	cmd.Flags().StringVar(&grantHost, "host", "", "--host=<host>")
	cmd.Flags().StringVar(&grantPrivs, "privs", "for example:SELECT,CREATE(comma-separated)", "--privs=<privs>")
	cmd.Flags().StringVar(&requireSSL, "ssl", "NO", "--ssl=<YES/NO>")

	return cmd
}

func mysqlCreateUserWithPrivilegesCommandFn(cmd *cobra.Command, args []string) {
	log.Warning("prepare.to.create.normaluser.[%v].with.privs", grantUser)
	conf, err := GetConfig()
	ErrorOK(err)

	self := conf.Server.Endpoint
	{
		leader, err := callx.GetClusterLeader(self)
		ErrorOK(err)
		rsp, err := callx.CreateUserWithPrivRPC(leader, grantUser, grantPasswd, grantDatabase, grantTable, grantHost, grantPrivs, requireSSL)
		ErrorOK(err)
		RspOK(rsp.RetCode)
	}
	log.Warning("create.normaluser[%v].with.privs.done", grantUser)
}

// NewMysqlGetUserCommand get mysql user list api (format in JSON)
func NewMysqlGetUserCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "getuser",
		Short: "get mysql user list",
		Run:   mysqlGetUserCommandFn,
	}

	return cmd
}

func mysqlGetUserCommandFn(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		ErrorOK(fmt.Errorf("too.many.args"))
	}

	conf, err := GetConfig()
	ErrorOK(err)

	self := conf.Server.Endpoint
	{
		leader, err := callx.GetClusterLeader(self)
		ErrorOK(err)
		rsp, err := callx.GetMysqlUserRPC(leader)
		ErrorOK(err)
		RspOK(rsp.RetCode)
		Users, _ := json.Marshal(rsp.Users)
		fmt.Printf("%s\n", string(Users))
	}
}
